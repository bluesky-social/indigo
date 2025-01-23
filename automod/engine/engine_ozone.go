package engine

import (
	"context"
	"fmt"
	"time"

	toolsozone "github.com/bluesky-social/indigo/api/ozone"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

func NewOzoneEventContext(ctx context.Context, eng *Engine, eventView *toolsozone.ModerationDefs_ModEventView) (*OzoneEventContext, error) {

	if eventView.Event == nil {
		return nil, fmt.Errorf("nil ozone event type")
	}

	eventType := ""
	if eventView.Event.ModerationDefs_ModEventTakedown != nil {
		eventType = "takedown"
	} else if eventView.Event.ModerationDefs_ModEventReverseTakedown != nil {
		eventType = "reverseTakedown"
	} else if eventView.Event.ModerationDefs_ModEventComment != nil {
		eventType = "comment"
	} else if eventView.Event.ModerationDefs_ModEventReport != nil {
		eventType = "report"
	} else if eventView.Event.ModerationDefs_ModEventLabel != nil {
		eventType = "label"
	} else if eventView.Event.ModerationDefs_ModEventAcknowledge != nil {
		eventType = "acknowledge"
	} else if eventView.Event.ModerationDefs_ModEventEscalate != nil {
		eventType = "escalate"
	} else if eventView.Event.ModerationDefs_ModEventMute != nil {
		eventType = "mute"
	} else if eventView.Event.ModerationDefs_ModEventUnmute != nil {
		eventType = "unmute"
	} else if eventView.Event.ModerationDefs_ModEventMuteReporter != nil {
		eventType = "muteReporter"
	} else if eventView.Event.ModerationDefs_ModEventUnmuteReporter != nil {
		eventType = "unmuteReporter"
	} else if eventView.Event.ModerationDefs_ModEventEmail != nil {
		eventType = "email"
	} else if eventView.Event.ModerationDefs_ModEventResolveAppeal != nil {
		eventType = "resolveAppeal"
	} else if eventView.Event.ModerationDefs_ModEventDivert != nil {
		eventType = "divert"
	} else if eventView.Event.ModerationDefs_ModEventTag != nil {
		eventType = "tag"
	} else {
		return nil, fmt.Errorf("unhandled ozone event type")
	}

	creatorDID, err := syntax.ParseDID(eventView.CreatedBy)
	if err != nil {
		return nil, err
	}

	var subjectDID syntax.DID
	var subjectURI *syntax.ATURI
	var recordMeta *RecordMeta
	if eventView.Subject == nil {
		return nil, fmt.Errorf("empty ozone event subject")
	} else if eventView.Subject.AdminDefs_RepoRef != nil {
		subjectDID, err = syntax.ParseDID(eventView.Subject.AdminDefs_RepoRef.Did)
		if err != nil {
			return nil, err
		}
	} else if eventView.Subject.RepoStrongRef != nil {
		u, err := syntax.ParseATURI(eventView.Subject.RepoStrongRef.Uri)
		if err != nil {
			return nil, err
		}
		subjectURI := &u
		subjectDID, err = subjectURI.Authority().AsDID()
		if err != nil {
			return nil, err
		}
		cidVal, err := syntax.ParseCID(eventView.Subject.RepoStrongRef.Cid)
		if err != nil {
			return nil, err
		}
		recordMeta = &RecordMeta{
			DID:        subjectDID,
			Collection: subjectURI.Collection(),
			RecordKey:  subjectURI.RecordKey(),
			CID:        &cidVal,
		}
	} else {
		return nil, fmt.Errorf("empty ozone event subject")
	}

	createdAt, err := syntax.ParseDatetime(eventView.CreatedAt)
	if err != nil {
		return nil, err
	}

	evt := OzoneEvent{
		EventType:  eventType,
		EventID:    eventView.Id,
		CreatedAt:  createdAt,
		CreatedBy:  creatorDID,
		SubjectDID: subjectDID,
		SubjectURI: subjectURI,
		// TODO: SubjectBlobs []syntax.CID
		Event: *eventView.Event,
	}

	creatorIdent, err := eng.Directory.LookupDID(ctx, evt.CreatedBy)
	if err != nil {
		return nil, err
	}
	if creatorIdent == nil {
		return nil, fmt.Errorf("identity not found for creator DID: %s", evt.CreatedBy)
	}
	creatorMeta, err := eng.GetAccountMeta(ctx, creatorIdent)
	if err != nil {
		return nil, err
	}

	subjectIdent, err := eng.Directory.LookupDID(ctx, evt.SubjectDID)
	if err != nil {
		return nil, err
	}
	if subjectIdent == nil {
		return nil, fmt.Errorf("identity not found for subject DID: %s", evt.SubjectDID)
	}
	accountMeta, err := eng.GetAccountMeta(ctx, subjectIdent)
	if err != nil {
		return nil, err
	}

	return &OzoneEventContext{
		AccountContext: AccountContext{
			BaseContext: BaseContext{
				Ctx:     ctx,
				Err:     nil,
				Logger:  eng.Logger.With("eventID", evt.EventID, "ozoneEventType", evt.EventType, "creatorDID", evt.CreatedBy, "subjectDID", evt.SubjectDID),
				engine:  eng,
				effects: &Effects{},
			},
			Account: *accountMeta,
		},
		Event:          evt,
		CreatorAccount: *creatorMeta,
		SubjectRecord:  recordMeta,
	}, nil
}

// Entrypoint for external code pushing ozone events.
//
// This method can be called concurrently, though cached state may end up inconsistent if multiple events for the same account (DID) are processed in parallel.
func (eng *Engine) ProcessOzoneEvent(ctx context.Context, eventView *toolsozone.ModerationDefs_ModEventView) error {
	eventProcessCount.WithLabelValues("ozone").Inc()
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		eventProcessDuration.WithLabelValues("ozone").Observe(duration.Seconds())
	}()

	// similar to an HTTP server, we want to recover any panics from rule execution
	defer func() {
		if r := recover(); r != nil {
			eng.Logger.Error("automod ozone event execution exception", "err", r, "eventID", eventView.Id, "createdAt", eventView.CreatedAt)
		}
	}()
	var cancel context.CancelFunc
	if eng.Config.OzoneEventTimeout != 0 {
		ctx, cancel = context.WithTimeout(ctx, eng.Config.OzoneEventTimeout)
		defer cancel()
	}

	ec, err := NewOzoneEventContext(ctx, eng, eventView)
	if err != nil {
		eventErrorCount.WithLabelValues("ozoneEvent").Inc()
		return fmt.Errorf("failed to hydrate ozone event context: %w", err)
	}

	// if this is a "self-event", created by automod itself, skip it to prevent a loop
	if ec.Event.CreatedBy.String() == eng.OzoneClient.Auth.Did {
		ec.Logger.Debug("skipping ozone self-event")
		return nil
	}

	ec.Logger.Debug("processing ozone event")

	if err := eng.Rules.CallOzoneEventRules(ec); err != nil {
		eventErrorCount.WithLabelValues("ozoneEvent").Inc()
		return fmt.Errorf("ozone rule execution failed: %w", err)
	}

	eng.CanonicalLogLineOzoneEvent(ec)

	// some ozone events should result in account meta cache flushes
	if (ec.Event.EventType == "takedown" || ec.Event.EventType == "reverseTakedown" || ec.Event.EventType == "label" || ec.Event.EventType == "tag") && ec.SubjectRecord == nil {
		if err := eng.PurgeAccountCaches(ctx, ec.Event.SubjectDID); err != nil {
			eng.Logger.Error("failed to purge identity cache", "err", err, "did", ec.Event.SubjectDID)
		}
	}
	if err := eng.persistAccountModActions(&ec.AccountContext); err != nil {
		eventErrorCount.WithLabelValues("ozoneEvent").Inc()
		return fmt.Errorf("failed to persist actions for ozone event: %w", err)
	}
	if err := eng.persistCounters(ctx, ec.effects); err != nil {
		eventErrorCount.WithLabelValues("ozoneEvent").Inc()
		return fmt.Errorf("failed to persist counts for ozone event: %w", err)
	}
	return nil
}

func (e *Engine) CanonicalLogLineOzoneEvent(c *OzoneEventContext) {
	c.Logger.Info("canonical-event-line",
		"accountLabels", c.effects.AccountLabels,
		"accountFlags", c.effects.AccountFlags,
		"accountTakedown", c.effects.AccountTakedown,
		"accountReports", len(c.effects.AccountReports),
		"recordLabels", c.effects.RecordLabels,
		"recordFlags", c.effects.RecordFlags,
		"recordTakedown", c.effects.RecordTakedown,
		"recordReports", len(c.effects.RecordReports),
	)
}
