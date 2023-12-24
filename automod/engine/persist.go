package engine

import (
	"context"
	"fmt"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/automod/util"
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
func (eng *Engine) persistAccountEffects(ctx context.Context, evt *RepoEvent, eff *Effects) error {

	// de-dupe actions
	newLabels := dedupeLabelActions(eff.AccountLabels, evt.Account.AccountLabels, evt.Account.AccountNegatedLabels)
	newFlags := dedupeFlagActions(eff.AccountFlags, evt.Account.AccountFlags)

	// don't report the same account multiple times on the same day for the same reason. this is a quick check; we also query the mod service API just before creating the report.
	newReports := eng.circuitBreakReports(eff, eng.dedupeReportActions(evt, eff, eff.AccountReports))
	newTakedown := eng.circuitBreakTakedown(eff, eff.AccountTakedown && !evt.Account.Takendown)

	anyModActions := newTakedown || len(newLabels) > 0 || len(newFlags) > 0 || len(newReports) > 0
	if anyModActions && eng.SlackWebhookURL != "" {
		msg := slackBody("⚠️ Automod Account Action ⚠️\n", evt.Account, newLabels, newFlags, newReports, newTakedown)
		if err := eng.SendSlackMsg(ctx, msg); err != nil {
			eng.Logger.Error("sending slack webhook", "err", err)
		}
	}

	// flags don't require admin auth
	if len(newFlags) > 0 {
		eng.Flags.Add(ctx, evt.Account.Identity.DID.String(), newFlags)
	}

	// if we can't actually talk to service, bail out early
	if eng.AdminClient == nil {
		return nil
	}

	xrpcc := eng.AdminClient

	if len(newLabels) > 0 {
		eng.Logger.Info("labeling record", "newLabels", newLabels)
		comment := "automod"
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
					Did: evt.Account.Identity.DID.String(),
				},
			},
		})
		if err != nil {
			return err
		}
	}

	// reports are additionally de-duped when persisting the action, so track with a flag
	createdReports := false
	for _, mr := range newReports {
		created, err := eng.createReportIfFresh(ctx, xrpcc, evt, mr)
		if err != nil {
			return err
		}
		if created {
			createdReports = true
		}
	}

	if newTakedown {
		eng.Logger.Warn("account-takedown")
		comment := "automod"
		_, err := comatproto.AdminEmitModerationEvent(ctx, xrpcc, &comatproto.AdminEmitModerationEvent_Input{
			CreatedBy: xrpcc.Auth.Did,
			Event: &comatproto.AdminEmitModerationEvent_Input_Event{
				AdminDefs_ModEventTakedown: &comatproto.AdminDefs_ModEventTakedown{
					Comment: &comment,
				},
			},
			Subject: &comatproto.AdminEmitModerationEvent_Input_Subject{
				AdminDefs_RepoRef: &comatproto.AdminDefs_RepoRef{
					Did: evt.Account.Identity.DID.String(),
				},
			},
		})
		if err != nil {
			return err
		}
	}

	needCachePurge := newTakedown || len(newLabels) > 0 || len(newFlags) > 0 || createdReports
	if needCachePurge {
		return eng.PurgeAccountCaches(ctx, evt.Account.Identity.DID)
	}

	return nil
}

// Persists some record-level state: labels, takedowns, reports.
//
// NOTE: this method currently does *not* persist record-level flags to any storage, and does not de-dupe most actions, on the assumption that the record is new (from firehose) and has no existing mod state.
func (eng *Engine) persistEffectss(ctx context.Context, evt *RecordEvent, eff *Effects) error {
	if err := eng.persistAccountEffects(ctx, &evt.RepoEvent, eff); err != nil {
		return err
	}

	// NOTE: record-level actions are *not* currently de-duplicated (aka, the same record could be labeled multiple times, or re-reported, etc)
	newLabels := util.DedupeStrings(eff.RecordLabels)
	newFlags := util.DedupeStrings(eff.RecordFlags)
	newReports := eng.circuitBreakReports(eff, eff.RecordReports)
	newTakedown := eng.circuitBreakTakedown(eff, eff.RecordTakedown)
	atURI := fmt.Sprintf("at://%s/%s/%s", evt.Account.Identity.DID, evt.Collection, evt.RecordKey)

	if newTakedown || len(newLabels) > 0 || len(newFlags) > 0 || len(newReports) > 0 {
		if eng.SlackWebhookURL != "" {
			msg := slackBody("⚠️ Automod Record Action ⚠️\n", evt.Account, newLabels, newFlags, newReports, newTakedown)
			msg += fmt.Sprintf("`%s`\n", atURI)
			if err := eng.SendSlackMsg(ctx, msg); err != nil {
				eng.Logger.Error("sending slack webhook", "err", err)
			}
		}
	}

	// flags don't require admin auth
	if len(newFlags) > 0 {
		eng.Flags.Add(ctx, atURI, newFlags)
	}

	if eng.AdminClient == nil {
		return nil
	}

	strongRef := comatproto.RepoStrongRef{
		Cid: evt.CID,
		Uri: atURI,
	}
	xrpcc := eng.AdminClient
	if len(newLabels) > 0 {
		eng.Logger.Info("labeling record", "newLabels", newLabels)
		comment := "automod"
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
			return err
		}
	}

	for _, mr := range newReports {
		eng.Logger.Info("reporting record", "reasonType", mr.ReasonType, "comment", mr.Comment)
		_, err := comatproto.ModerationCreateReport(ctx, xrpcc, &comatproto.ModerationCreateReport_Input{
			ReasonType: &mr.ReasonType,
			Reason:     &mr.Comment,
			Subject: &comatproto.ModerationCreateReport_Input_Subject{
				RepoStrongRef: &strongRef,
			},
		})
		if err != nil {
			return err
		}
	}
	if newTakedown {
		eng.Logger.Warn("record-takedown")
		comment := "automod"
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
		})
		if err != nil {
			return err
		}
	}
	return nil
}
