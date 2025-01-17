package engine

import (
	"context"
	"fmt"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	toolsozone "github.com/bluesky-social/indigo/api/ozone"
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

// Persists account-level moderation actions: new labels, new tags, new flags, new takedowns, and reports.
//
// If necessary, will "purge" identity and account caches, so that state updates will be picked up for subsequent events.
//
// Note that this method expects to run *before* counts are persisted (it accesses and updates some counts)
func (eng *Engine) persistAccountModActions(c *AccountContext) error {
	ctx := c.Ctx

	// de-dupe actions
	newLabels := dedupeLabelActions(c.effects.AccountLabels, c.Account.AccountLabels, c.Account.AccountNegatedLabels)
	existingTags := []string{}
	if c.Account.Private != nil {
		existingTags = c.Account.Private.AccountTags
	}
	newTags := dedupeTagActions(c.effects.AccountTags, existingTags)
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
	newEscalation := c.effects.AccountEscalate
	if c.Account.Private != nil && c.Account.Private.ReviewState == ReviewStateEscalated {
		// de-dupe account escalation
		newEscalation = false
	} else {
		newEscalation, err = eng.circuitBreakModAction(ctx, newEscalation)
		if err != nil {
			return fmt.Errorf("circuit-breaking escalation: %w", err)
		}
	}
	newAcknowledge := c.effects.AccountAcknowledge
	if c.Account.Private != nil && (c.Account.Private.ReviewState == "closed" || c.Account.Private.ReviewState == "none") {
		// de-dupe account escalation
		newAcknowledge = false
	} else {
		newAcknowledge, err = eng.circuitBreakModAction(ctx, newAcknowledge)
		if err != nil {
			return fmt.Errorf("circuit-breaking acknowledge: %w", err)
		}
	}

	anyModActions := newTakedown || newEscalation || newAcknowledge || len(newLabels) > 0 || len(newTags) > 0 || len(newFlags) > 0 || len(newReports) > 0
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
		c.Logger.Info("labeling account", "newLabels", newLabels)
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

	if len(newTags) > 0 {
		c.Logger.Info("tagging account", "newTags", newTags)
		for _, val := range newTags {
			// note: WithLabelValues is a prometheus label, not an atproto label
			actionNewTagCount.WithLabelValues("account", val).Inc()
		}
		comment := "[automod]: auto-tagging account"
		_, err := toolsozone.ModerationEmitEvent(ctx, xrpcc, &toolsozone.ModerationEmitEvent_Input{
			CreatedBy: xrpcc.Auth.Did,
			Event: &toolsozone.ModerationEmitEvent_Input_Event{
				ModerationDefs_ModEventTag: &toolsozone.ModerationDefs_ModEventTag{
					Add:     newTags,
					Remove:  []string{},
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
			c.Logger.Error("failed to create account tags", "err", err)
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

		// we don't want to escalate if there is a takedown
		newEscalation = false
	}

	if newEscalation {
		c.Logger.Info("account-escalate")
		actionNewEscalationCount.WithLabelValues("account").Inc()
		comment := "[automod]: auto account-escalation"
		_, err := toolsozone.ModerationEmitEvent(ctx, xrpcc, &toolsozone.ModerationEmitEvent_Input{
			CreatedBy: xrpcc.Auth.Did,
			Event: &toolsozone.ModerationEmitEvent_Input_Event{
				ModerationDefs_ModEventEscalate: &toolsozone.ModerationDefs_ModEventEscalate{
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
			c.Logger.Error("failed to execute account escalation", "err", err)
		}
	}

	if newAcknowledge {
		c.Logger.Info("account-acknowledge")
		actionNewAcknowledgeCount.WithLabelValues("account").Inc()
		comment := "[automod]: auto account-acknowledge"
		_, err := toolsozone.ModerationEmitEvent(ctx, xrpcc, &toolsozone.ModerationEmitEvent_Input{
			CreatedBy: xrpcc.Auth.Did,
			Event: &toolsozone.ModerationEmitEvent_Input_Event{
				ModerationDefs_ModEventAcknowledge: &toolsozone.ModerationDefs_ModEventAcknowledge{
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
			c.Logger.Error("failed to execute account acknowledge", "err", err)
		}
	}

	needCachePurge := newTakedown || newEscalation || newAcknowledge || len(newLabels) > 0 || len(newTags) > 0 || len(newFlags) > 0 || createdReports
	if needCachePurge {
		return eng.PurgeAccountCaches(ctx, c.Account.Identity.DID)
	}

	return nil
}

// Persists some record-level state: labels, tags, takedowns, reports.
//
// NOTE: this method currently does *not* persist record-level flags to any storage, and does not de-dupe most actions, on the assumption that the record is new (from firehose) and has no existing mod state.
func (eng *Engine) persistRecordModActions(c *RecordContext) error {
	ctx := c.Ctx
	if err := eng.persistAccountModActions(&c.AccountContext); err != nil {
		return err
	}

	atURI := c.RecordOp.ATURI().String()
	newLabels := dedupeStrings(c.effects.RecordLabels)
	newTags := dedupeStrings(c.effects.RecordTags)
	newEscalation := c.effects.RecordEscalate
	newAcknowledge := c.effects.RecordAcknowledge

	if (newEscalation || newAcknowledge || len(newLabels) > 0 || len(newTags) > 0) && eng.OzoneClient != nil {
		// fetch existing record labels, tags, etc
		rv, err := toolsozone.ModerationGetRecord(ctx, eng.OzoneClient, c.RecordOp.CID.String(), c.RecordOp.ATURI().String())
		if err != nil {
			// NOTE: there is a frequent 4xx error here from Ozone because this record has not been indexed yet
			c.Logger.Warn("failed to fetch private record metadata from Ozone", "err", err)
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
			newLabels = dedupeLabelActions(newLabels, existingLabels, negLabels)
			existingTags := []string{}
			hasSubjectStatus := rv.Moderation != nil && rv.Moderation.SubjectStatus != nil
			if hasSubjectStatus && rv.Moderation.SubjectStatus.Tags != nil {
				existingTags = rv.Moderation.SubjectStatus.Tags
			}
			newTags = dedupeTagActions(newTags, existingTags)
			newEscalation = newEscalation && hasSubjectStatus && *rv.Moderation.SubjectStatus.ReviewState != "tools.ozone.moderation.defs#reviewEscalate"
			newAcknowledge = newAcknowledge && hasSubjectStatus && *rv.Moderation.SubjectStatus.ReviewState != "tools.ozone.moderation.defs#reviewNone" && *rv.Moderation.SubjectStatus.ReviewState != "tools.ozone.moderation.defs#reviewClosed"
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
	newEscalation, err = eng.circuitBreakModAction(ctx, newEscalation)
	if err != nil {
		return fmt.Errorf("circuit-breaking escalation: %w", err)
	}
	newAcknowledge, err = eng.circuitBreakModAction(ctx, newAcknowledge)
	if err != nil {
		return fmt.Errorf("circuit-breaking acknowledge: %w", err)
	}

	if newEscalation || newAcknowledge || newTakedown || len(newLabels) > 0 || len(newTags) > 0 || len(newFlags) > 0 || len(newReports) > 0 {
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
	if !newAcknowledge && !newEscalation && !newTakedown && len(newLabels) == 0 && len(newTags) == 0 && len(newReports) == 0 {
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

	if len(newTags) > 0 {
		c.Logger.Info("tagging record", "newTags", newTags)
		for _, val := range newTags {
			// note: WithLabelValues is a prometheus label, not an atproto label
			actionNewTagCount.WithLabelValues("record", val).Inc()
		}
		comment := "[automod]: auto-tagging record"
		_, err := toolsozone.ModerationEmitEvent(ctx, xrpcc, &toolsozone.ModerationEmitEvent_Input{
			CreatedBy: xrpcc.Auth.Did,
			Event: &toolsozone.ModerationEmitEvent_Input_Event{
				ModerationDefs_ModEventTag: &toolsozone.ModerationDefs_ModEventTag{
					Add:     newTags,
					Remove:  []string{},
					Comment: &comment,
				},
			},
			Subject: &toolsozone.ModerationEmitEvent_Input_Subject{
				RepoStrongRef: &strongRef,
			},
		})
		if err != nil {
			c.Logger.Error("failed to create record tag", "err", err)
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

		// we don't want to escalate if there is a takedown
		newEscalation = false
	}

	if newEscalation {
		c.Logger.Warn("record-escalation")
		actionNewEscalationCount.WithLabelValues("record").Inc()
		comment := "[automod]: automated record-escalation"
		_, err := toolsozone.ModerationEmitEvent(ctx, xrpcc, &toolsozone.ModerationEmitEvent_Input{
			CreatedBy: xrpcc.Auth.Did,
			Event: &toolsozone.ModerationEmitEvent_Input_Event{
				ModerationDefs_ModEventEscalate: &toolsozone.ModerationDefs_ModEventEscalate{
					Comment: &comment,
				},
			},
			Subject: &toolsozone.ModerationEmitEvent_Input_Subject{
				RepoStrongRef: &strongRef,
			},
		})
		if err != nil {
			c.Logger.Error("failed to execute record escalation", "err", err)
		}
	}

	if newAcknowledge {
		c.Logger.Warn("record-acknowledge")
		actionNewAcknowledgeCount.WithLabelValues("record").Inc()
		comment := "[automod]: automated record-acknowledge"
		_, err := toolsozone.ModerationEmitEvent(ctx, xrpcc, &toolsozone.ModerationEmitEvent_Input{
			CreatedBy: xrpcc.Auth.Did,
			Event: &toolsozone.ModerationEmitEvent_Input_Event{
				ModerationDefs_ModEventAcknowledge: &toolsozone.ModerationDefs_ModEventAcknowledge{
					Comment: &comment,
				},
			},
			Subject: &toolsozone.ModerationEmitEvent_Input_Subject{
				RepoStrongRef: &strongRef,
			},
		})
		if err != nil {
			c.Logger.Error("failed to execute record acknowledge", "err", err)
		}
	}
	return nil
}
