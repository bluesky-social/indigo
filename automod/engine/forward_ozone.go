package engine

import (
	"context"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	toolsozone "github.com/bluesky-social/indigo/api/ozone"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

func (eng *Engine) ForwardOzoneAccountEvent(ctx context.Context, e *comatproto.SyncSubscribeRepos_Account) error {
	comment := "[automod]: Account status event"
	eng.forwardOzoneEvent(ctx, toolsozone.ModerationEmitEvent_Input_Event{
		ModerationDefs_AccountEvent: &toolsozone.ModerationDefs_AccountEvent{
			Comment:   &comment,
			Timestamp: e.Time,
			Status:    e.Status,
			Active:    e.Active,
		},
	}, toolsozone.ModerationEmitEvent_Input_Subject{
		AdminDefs_RepoRef: &comatproto.AdminDefs_RepoRef{
			Did: e.Did,
		},
	})
	return nil
}

func (eng *Engine) ForwardOzoneIdentityEvent(ctx context.Context, e *comatproto.SyncSubscribeRepos_Identity) error {
	comment := "[automod]: Identity event"
	ident, err := eng.Directory.LookupDID(ctx, syntax.DID(e.Did))
	if err != nil {
		return err
	}
	handleStr := ident.Handle.String()
	pdsStr := ident.PDSEndpoint()

	eng.forwardOzoneEvent(ctx, toolsozone.ModerationEmitEvent_Input_Event{
		ModerationDefs_IdentityEvent: &toolsozone.ModerationDefs_IdentityEvent{
			Comment: &comment,
			Handle:  &handleStr,
			PdsHost: &pdsStr,
			//Tombstone
			Timestamp: e.Time,
		},
	}, toolsozone.ModerationEmitEvent_Input_Subject{
		AdminDefs_RepoRef: &comatproto.AdminDefs_RepoRef{
			Did: e.Did,
		},
	})

	return nil
}

// For the given subject, checks if there is already an event of the given type within the 5 minutes.
func (eng *Engine) isDupeOzoneEvent(ctx context.Context, event toolsozone.ModerationEmitEvent_Input_Event, subject toolsozone.ModerationEmitEvent_Input_Subject) (bool, error) {
	if eng.OzoneClient == nil {
		eng.Logger.Warn("can not check if event is duplicate, mod service client not configured")
		return false, nil
	}

	eventType := ""
	if event.ModerationDefs_AccountEvent != nil {
		eventType = "tools.ozone.moderation.defs#accountEvent"
	} else if event.ModerationDefs_IdentityEvent != nil {
		eventType = "tools.ozone.moderation.defs#identityEvent"
	} else if event.ModerationDefs_RecordEvent != nil {
		eventType = "tools.ozone.moderation.defs#recordEvent"
	}

	eventSubject := ""
	if subject.AdminDefs_RepoRef != nil {
		eventSubject = subject.AdminDefs_RepoRef.Did
	} else if subject.RepoStrongRef != nil {
		eventSubject = subject.RepoStrongRef.Uri
	}

	after := time.Now().Add(-time.Minute * 5).Format(time.RFC3339)
	xrpcc := eng.OzoneClient
	resp, err := toolsozone.ModerationQueryEvents(
		ctx,
		xrpcc,
		nil,                 // addedLabels []string
		nil,                 // addedTags []string
		nil,                 // collections []string
		"",                  // comment string
		after,               // createdAfter string
		"",                  // createdBefore string
		"",                  // createdBy string
		"",                  // cursor string
		false,               // hasComment bool
		false,               // includeAllUserRecords bool
		1,                   // limit int64
		nil,                 // policies []string
		nil,                 // removedLabels []string
		nil,                 // removedTags []string
		nil,                 // reportTypes []string
		"",                  // sortDirection string
		eventSubject,        // subject string
		"",                  // subjectType string
		[]string{eventType}, // types []string
	)

	if err != nil {
		eng.Logger.Error("failed to query events", "err", err)
		return false, err
	}

	if len(resp.Events) > 0 {
		eng.Logger.Info("similar ozone event was forwarded recently, skipping forward", "eventType", eventType, "eventSubject", eventSubject, "evt", resp.Events[0], "after", after)
		return true, nil
	}

	return false, nil
}

func (eng *Engine) forwardOzoneEvent(ctx context.Context, event toolsozone.ModerationEmitEvent_Input_Event, subject toolsozone.ModerationEmitEvent_Input_Subject) error {
	// if we can't actually talk to service, bail out early
	if eng.OzoneClient == nil {
		eng.Logger.Warn("not persisting ozone account event; mod service client not configured")
		return nil
	}

	isDuplicate, err := eng.isDupeOzoneEvent(ctx, event, subject)
	if err != nil {
		eng.Logger.Error("failed to check if event is duplicate", "err", err)
		return err
	}

	if isDuplicate {
		return nil
	}

	xrpcc := eng.OzoneClient
	_, err = toolsozone.ModerationEmitEvent(ctx, xrpcc, &toolsozone.ModerationEmitEvent_Input{
		CreatedBy: xrpcc.Auth.Did,
		Event:     &event,
		Subject:   &subject,
	})
	if err != nil {
		eng.Logger.Error("failed to forward event to ozone", "err", err)
		return err
	}

	return nil
}
