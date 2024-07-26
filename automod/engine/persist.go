package engine

import (
	"context"
	"fmt"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	toolsozone "github.com/bluesky-social/indigo/api/ozone"
	"github.com/davecgh/go-spew/spew"
)

func (eng *Engine) persistCounters(ctx context.Context, eff *Effects) error {
	// TODO: dedupe this array
	for _, ref := range eff.CounterIncrements {
		if ref.Period != nil {
			err := eng.Counters.IncrementPeriod(ctx, ref.Name, ref.Val, *ref.Period)
			if err != nil {
				return err
			}
		} else {
			err := eng.Counters.Increment(ctx, ref.Name, ref.Val)
			if err != nil {
				return err
			}
		}
	}
	for _, ref := range eff.CounterDistinctIncrements {
		err := eng.Counters.IncrementDistinct(ctx, ref.Name, ref.Bucket, ref.Val)
		if err != nil {
			return err
		}
	}
	return nil
}

// Persists account-level moderation actions: new labels, new flags, new takedowns, and reports.
//
// If necessary, will "purge" identity and account caches, so that state updates will be picked up for subsequent events.
//
// Note that this method expects to run *before* counts are persisted (it accesses and updates some counts)
func (eng *Engine) persistAccountModActions(c *AccountContext) error {
	ctx := c.Ctx

	// de-dupe actions
	newLabels := dedupeLabelActions(c.effects.AccountLabels, c.Account.AccountLabels, c.Account.AccountNegatedLabels)
	newFlags := dedupeFlagActions(c.effects.AccountFlags, c.Account.AccountFlags)

	// don't report the same account multiple times on the same day for the same reason. this is a quick check; we also query the mod service API just before creating the report.
	partialReports, err := eng.dedupeReportActions(ctx, c.Account.Identity.DID.String(), c.effects.AccountReports)
	if err != nil {
		return fmt.Errorf("de-duplicating reports: %w", err)
	}
	newReports, err := eng.circuitBreakReports(ctx, partialReports)
	if err != nil {
		return fmt.Errorf("circuit-breaking reports: %w", err)
	}
	newTakedown, err := eng.circuitBreakTakedown(ctx, c.effects.AccountTakedown && !c.Account.Takendown)
	if err != nil {
		return fmt.Errorf("circuit-breaking takedowns: %w", err)
	}

	anyModActions := newTakedown || len(newLabels) > 0 || len(newFlags) > 0 || len(newReports) > 0
	if anyModActions && eng.Notifier != nil {
		for _, srv := range dedupeStrings(c.effects.NotifyServices) {
			if err := eng.Notifier.SendAccount(ctx, srv, c); err != nil {
				c.Logger.Error("failed to deliver notification", "service", srv, "err", err)
			}
		}
	}

	// flags don't require admin auth
	if len(newFlags) > 0 {
		for _, val := range newFlags {
			// note: WithLabelValues is a prometheus label, not an atproto label
			actionNewFlagCount.WithLabelValues("record", val).Inc()
		}
		eng.Flags.Add(ctx, c.Account.Identity.DID.String(), newFlags)
	}

	// if we can't actually talk to service, bail out early
	if eng.OzoneClient == nil {
		if anyModActions {
			c.Logger.Warn("not persisting actions, mod service client not configured")
		}
		return nil
	}

	xrpcc := eng.OzoneClient

	if len(newLabels) > 0 {
		c.Logger.Info("labeling record", "newLabels", newLabels)
		for _, val := range newLabels {
			// note: WithLabelValues is a prometheus label, not an atproto label
			actionNewLabelCount.WithLabelValues("account", val).Inc()
		}
		comment := "[automod]: auto-labeling account"
		_, err := toolsozone.ModerationEmitEvent(ctx, xrpcc, &toolsozone.ModerationEmitEvent_Input{
			CreatedBy: xrpcc.Auth.Did,
			Event: &toolsozone.ModerationEmitEvent_Input_Event{
				ModerationDefs_ModEventLabel: &toolsozone.ModerationDefs_ModEventLabel{
					CreateLabelVals: newLabels,
					NegateLabelVals: []string{},
					Comment:         &comment,
				},
			},
			Subject: &toolsozone.ModerationEmitEvent_Input_Subject{
				AdminDefs_RepoRef: &comatproto.AdminDefs_RepoRef{
					Did: c.Account.Identity.DID.String(),
				},
			},
		})
		if err != nil {
			c.Logger.Error("failed to create account labels", "err", err)
		}
	}

	// reports are additionally de-duped when persisting the action, so track with a flag
	createdReports := false
	for _, mr := range newReports {
		created, err := eng.createReportIfFresh(ctx, xrpcc, c.Account.Identity.DID, mr)
		if err != nil {
			c.Logger.Error("failed to create account report", "err", err)
		}
		if created {
			createdReports = true
		}
	}

	if newTakedown {
		c.Logger.Warn("account-takedown")
		actionNewTakedownCount.WithLabelValues("account").Inc()
		comment := "[automod]: auto account-takedown"
		_, err := toolsozone.ModerationEmitEvent(ctx, xrpcc, &toolsozone.ModerationEmitEvent_Input{
			CreatedBy: xrpcc.Auth.Did,
			Event: &toolsozone.ModerationEmitEvent_Input_Event{
				ModerationDefs_ModEventTakedown: &toolsozone.ModerationDefs_ModEventTakedown{
					Comment: &comment,
				},
			},
			Subject: &toolsozone.ModerationEmitEvent_Input_Subject{
				AdminDefs_RepoRef: &comatproto.AdminDefs_RepoRef{
					Did: c.Account.Identity.DID.String(),
				},
			},
		})
		if err != nil {
			c.Logger.Error("failed to execute account takedown", "err", err)
		}
	}

	needCachePurge := newTakedown || len(newLabels) > 0 || len(newFlags) > 0 || createdReports
	if needCachePurge {
		return eng.PurgeAccountCaches(ctx, c.Account.Identity.DID)
	}

	return nil
}

func (eng *Engine) persistOzoneAccountEvent(c *AccountContext, typ string) error {
	ctx := c.Ctx

	c.Logger.Info("ozone account event handle", "typ", typ)
	comment := "[automod]: account event"
	var event toolsozone.ModerationEmitEvent_Input_Event
	spew.Dump(c.Account)

	// @TODO: Add duplicate check here so that the same event is not emitted more than once

	// @TODO: Inside of each of these blocks we would define a different type of event, for now, these are just comment events
	if typ == "handle" {
		comment = "[automod]: handle update"
		event = toolsozone.ModerationEmitEvent_Input_Event{
			ModerationDefs_ModEventComment: &toolsozone.ModerationDefs_ModEventComment{
				Comment: comment,
			},
		}
	} else if typ == "tombstone" {
		comment = "[automod]: account deleted"
		event = toolsozone.ModerationEmitEvent_Input_Event{
			ModerationDefs_ModEventComment: &toolsozone.ModerationDefs_ModEventComment{
				Comment: comment,
			},
		}
	} else if typ == "account" {
		// @TODO: Could there be other reasons for this event to fire? if so, we should be able to discard those and only handle activation/reactivation
		if c.Account.Deactivated {
			comment = "[automod]: account deactivated"
		} else {
			comment = "[automod]: account re-activated"
		}

		event = toolsozone.ModerationEmitEvent_Input_Event{
			ModerationDefs_ModEventComment: &toolsozone.ModerationDefs_ModEventComment{
				Comment: comment,
			},
		}
	} else {
		// Silently ignoring unknown event types
		return nil
	}

	c.Logger.Info("comment for account event", "comment", comment)

	// if we can't actually talk to service, bail out early
	if eng.OzoneClient == nil {
		c.Logger.Warn("not persisting ozone account event, mod service client not configured")
		return nil
	}

	xrpcc := eng.OzoneClient
	_, err := toolsozone.ModerationEmitEvent(ctx, xrpcc, &toolsozone.ModerationEmitEvent_Input{
		CreatedBy: xrpcc.Auth.Did,
		Event:     &event,
		Subject: &toolsozone.ModerationEmitEvent_Input_Subject{
			AdminDefs_RepoRef: &comatproto.AdminDefs_RepoRef{
				Did: c.Account.Identity.DID.String(),
			},
		},
	})
	if err != nil {
		c.Logger.Error("failed to send account event", "err", err)
	}

	return nil
}

// Persists some record-level state: labels, takedowns, reports.
//
// NOTE: this method currently does *not* persist record-level flags to any storage, and does not de-dupe most actions, on the assumption that the record is new (from firehose) and has no existing mod state.
func (eng *Engine) persistRecordModActions(c *RecordContext) error {
	ctx := c.Ctx
	if err := eng.persistAccountModActions(&c.AccountContext); err != nil {
		return err
	}

	atURI := c.RecordOp.ATURI().String()
	newLabels := dedupeStrings(c.effects.RecordLabels)
	if len(newLabels) > 0 && eng.OzoneClient != nil {
		rv, err := toolsozone.ModerationGetRecord(ctx, eng.OzoneClient, c.RecordOp.CID.String(), c.RecordOp.ATURI().String())
		if err != nil {
			// NOTE: there is a frequent 4xx error here from Ozone because this record has not been indexed yet
			c.Logger.Warn("failed to fetch private record metadata", "err", err)
		} else {
			var existingLabels []string
			var negLabels []string
			for _, lbl := range rv.Labels {
				if lbl.Neg != nil && *lbl.Neg == true {
					negLabels = append(negLabels, lbl.Val)
				} else {
					existingLabels = append(existingLabels, lbl.Val)
				}
			}
			existingLabels = dedupeStrings(existingLabels)
			negLabels = dedupeStrings(negLabels)
			// fetch existing record labels
			newLabels = dedupeLabelActions(newLabels, existingLabels, negLabels)
		}
	}
	newFlags := dedupeStrings(c.effects.RecordFlags)
	if len(newFlags) > 0 {
		// fetch existing flags, and de-dupe
		existingFlags, err := eng.Flags.Get(ctx, atURI)
		if err != nil {
			return fmt.Errorf("failed checking record flag cache: %w", err)
		}
		newFlags = dedupeFlagActions(newFlags, existingFlags)
	}

	// don't report the same record multiple times on the same day for the same reason. this is a quick check; we also query the mod service API just before creating the report.
	partialReports, err := eng.dedupeReportActions(ctx, atURI, c.effects.RecordReports)
	if err != nil {
		return fmt.Errorf("de-duplicating reports: %w", err)
	}
	newReports, err := eng.circuitBreakReports(ctx, partialReports)
	if err != nil {
		return fmt.Errorf("failed to circuit break reports: %w", err)
	}
	newTakedown, err := eng.circuitBreakTakedown(ctx, c.effects.RecordTakedown)
	if err != nil {
		return fmt.Errorf("failed to circuit break takedowns: %w", err)
	}

	if newTakedown || len(newLabels) > 0 || len(newFlags) > 0 || len(newReports) > 0 {
		if eng.Notifier != nil {
			for _, srv := range dedupeStrings(c.effects.NotifyServices) {
				if err := eng.Notifier.SendRecord(ctx, srv, c); err != nil {
					c.Logger.Error("failed to deliver notification", "service", srv, "err", err)
				}
			}
		}
	}

	// flags don't require admin auth
	if len(newFlags) > 0 {
		for _, val := range newFlags {
			// note: WithLabelValues is a prometheus label, not an atproto label
			actionNewFlagCount.WithLabelValues("record", val).Inc()
		}
		eng.Flags.Add(ctx, atURI, newFlags)
	}

	// exit early
	if !newTakedown && len(newLabels) == 0 && len(newReports) == 0 {
		return nil
	}

	if eng.OzoneClient == nil {
		c.Logger.Warn("not persisting actions because mod service client not configured")
		return nil
	}

	if c.RecordOp.CID == nil {
		c.Logger.Warn("skipping record actions because CID is nil, can't construct strong ref")
		return nil
	}
	cid := *c.RecordOp.CID
	strongRef := comatproto.RepoStrongRef{
		Cid: cid.String(),
		Uri: atURI,
	}

	xrpcc := eng.OzoneClient
	if len(newLabels) > 0 {
		c.Logger.Info("labeling record", "newLabels", newLabels)
		for _, val := range newLabels {
			// note: WithLabelValues is a prometheus label, not an atproto label
			actionNewLabelCount.WithLabelValues("record", val).Inc()
		}
		comment := "[automod]: auto-labeling record"
		_, err := toolsozone.ModerationEmitEvent(ctx, xrpcc, &toolsozone.ModerationEmitEvent_Input{
			CreatedBy: xrpcc.Auth.Did,
			Event: &toolsozone.ModerationEmitEvent_Input_Event{
				ModerationDefs_ModEventLabel: &toolsozone.ModerationDefs_ModEventLabel{
					CreateLabelVals: newLabels,
					NegateLabelVals: []string{},
					Comment:         &comment,
				},
			},
			Subject: &toolsozone.ModerationEmitEvent_Input_Subject{
				RepoStrongRef: &strongRef,
			},
		})
		if err != nil {
			c.Logger.Error("failed to create record label", "err", err)
		}
	}

	for _, mr := range newReports {
		_, err := eng.createRecordReportIfFresh(ctx, xrpcc, c.RecordOp.ATURI(), c.RecordOp.CID, mr)
		if err != nil {
			c.Logger.Error("failed to create record report", "err", err)
		}
	}

	if newTakedown {
		c.Logger.Warn("record-takedown")
		actionNewTakedownCount.WithLabelValues("record").Inc()
		comment := "[automod]: automated record-takedown"
		_, err := toolsozone.ModerationEmitEvent(ctx, xrpcc, &toolsozone.ModerationEmitEvent_Input{
			CreatedBy: xrpcc.Auth.Did,
			Event: &toolsozone.ModerationEmitEvent_Input_Event{
				ModerationDefs_ModEventTakedown: &toolsozone.ModerationDefs_ModEventTakedown{
					Comment: &comment,
				},
			},
			Subject: &toolsozone.ModerationEmitEvent_Input_Subject{
				RepoStrongRef: &strongRef,
			},
			SubjectBlobCids: dedupeStrings(c.effects.BlobTakedowns),
		})
		if err != nil {
			c.Logger.Error("failed to execute record takedown", "err", err)
		}
	}
	return nil
}
