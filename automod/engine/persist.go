package engine

import (
	"context"
	"fmt"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
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
	partialReports, err := eng.dedupeReportActions(ctx, c.Account.Identity.DID, c.effects.AccountReports)
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
	if anyModActions && eng.SlackWebhookURL != "" {
		msg := slackBody("⚠️ Automod Account Action ⚠️\n", c.Account, newLabels, newFlags, newReports, newTakedown)
		if err := eng.SendSlackMsg(ctx, msg); err != nil {
			c.Logger.Error("sending slack webhook", "err", err)
		}
	}

	// flags don't require admin auth
	if len(newFlags) > 0 {
		eng.Flags.Add(ctx, c.Account.Identity.DID.String(), newFlags)
	}

	// if we can't actually talk to service, bail out early
	if eng.AdminClient == nil {
		if anyModActions {
			c.Logger.Warn("not persisting actions, mod service client not configured")
		}
		return nil
	}

	xrpcc := eng.AdminClient

	if len(newLabels) > 0 {
		c.Logger.Info("labeling record", "newLabels", newLabels)
		comment := "[automod]: auto-labeling account"
		_, err := comatproto.AdminEmitModerationEvent(ctx, xrpcc, &comatproto.AdminEmitModerationEvent_Input{
			CreatedBy: xrpcc.Auth.Did,
			Event: &comatproto.AdminEmitModerationEvent_Input_Event{
				AdminDefs_ModEventLabel: &comatproto.AdminDefs_ModEventLabel{
					CreateLabelVals: newLabels,
					NegateLabelVals: []string{},
					Comment:         &comment,
				},
			},
			Subject: &comatproto.AdminEmitModerationEvent_Input_Subject{
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
		comment := "[automod]: auto account-takedown"
		_, err := comatproto.AdminEmitModerationEvent(ctx, xrpcc, &comatproto.AdminEmitModerationEvent_Input{
			CreatedBy: xrpcc.Auth.Did,
			Event: &comatproto.AdminEmitModerationEvent_Input_Event{
				AdminDefs_ModEventTakedown: &comatproto.AdminDefs_ModEventTakedown{
					Comment: &comment,
				},
			},
			Subject: &comatproto.AdminEmitModerationEvent_Input_Subject{
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

// Persists some record-level state: labels, takedowns, reports.
//
// NOTE: this method currently does *not* persist record-level flags to any storage, and does not de-dupe most actions, on the assumption that the record is new (from firehose) and has no existing mod state.
func (eng *Engine) persistRecordModActions(c *RecordContext) error {
	ctx := c.Ctx
	if err := eng.persistAccountModActions(&c.AccountContext); err != nil {
		return err
	}

	// NOTE: record-level actions are *not* currently de-duplicated (aka, the same record could be labeled multiple times, or re-reported, etc)
	newLabels := dedupeStrings(c.effects.RecordLabels)
	newFlags := dedupeStrings(c.effects.RecordFlags)
	newReports, err := eng.circuitBreakReports(ctx, c.effects.RecordReports)
	if err != nil {
		return fmt.Errorf("failed to circuit break reports: %w", err)
	}
	newTakedown, err := eng.circuitBreakTakedown(ctx, c.effects.RecordTakedown)
	if err != nil {
		return fmt.Errorf("failed to circuit break takedowns: %w", err)
	}
	atURI := fmt.Sprintf("at://%s/%s/%s", c.Account.Identity.DID, c.RecordOp.Collection, c.RecordOp.RecordKey)

	if newTakedown || len(newLabels) > 0 || len(newFlags) > 0 || len(newReports) > 0 {
		if eng.SlackWebhookURL != "" {
			msg := slackBody("⚠️ Automod Record Action ⚠️\n", c.Account, newLabels, newFlags, newReports, newTakedown)
			msg += fmt.Sprintf("`%s`\n", atURI)
			if err := eng.SendSlackMsg(ctx, msg); err != nil {
				c.Logger.Error("sending slack webhook", "err", err)
			}
		}
	}

	// flags don't require admin auth
	if len(newFlags) > 0 {
		eng.Flags.Add(ctx, atURI, newFlags)
	}

	// exit early
	if !newTakedown && len(newLabels) == 0 && len(newReports) == 0 {
		return nil
	}

	if eng.AdminClient == nil {
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

	xrpcc := eng.AdminClient
	if len(newLabels) > 0 {
		c.Logger.Info("labeling record", "newLabels", newLabels)
		comment := "[automod]: auto-labeling record"
		_, err := comatproto.AdminEmitModerationEvent(ctx, xrpcc, &comatproto.AdminEmitModerationEvent_Input{
			CreatedBy: xrpcc.Auth.Did,
			Event: &comatproto.AdminEmitModerationEvent_Input_Event{
				AdminDefs_ModEventLabel: &comatproto.AdminDefs_ModEventLabel{
					CreateLabelVals: newLabels,
					NegateLabelVals: []string{},
					Comment:         &comment,
				},
			},
			Subject: &comatproto.AdminEmitModerationEvent_Input_Subject{
				RepoStrongRef: &strongRef,
			},
		})
		if err != nil {
			c.Logger.Error("failed to create record label", "err", err)
		}
	}

	for _, mr := range newReports {
		c.Logger.Info("reporting record", "reasonType", mr.ReasonType, "comment", mr.Comment)
		comment := "[automod] " + mr.Comment
		_, err := comatproto.ModerationCreateReport(ctx, xrpcc, &comatproto.ModerationCreateReport_Input{
			ReasonType: &mr.ReasonType,
			Reason:     &comment,
			Subject: &comatproto.ModerationCreateReport_Input_Subject{
				RepoStrongRef: &strongRef,
			},
		})
		if err != nil {
			c.Logger.Error("failed to create record report", "err", err)
		}
	}
	if newTakedown {
		c.Logger.Warn("record-takedown")
		comment := "[automod]: automated record-takedown"
		_, err := comatproto.AdminEmitModerationEvent(ctx, xrpcc, &comatproto.AdminEmitModerationEvent_Input{
			CreatedBy: xrpcc.Auth.Did,
			Event: &comatproto.AdminEmitModerationEvent_Input_Event{
				AdminDefs_ModEventTakedown: &comatproto.AdminDefs_ModEventTakedown{
					Comment: &comment,
				},
			},
			Subject: &comatproto.AdminEmitModerationEvent_Input_Subject{
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
