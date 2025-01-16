package engine

import (
	"context"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	toolsozone "github.com/bluesky-social/indigo/api/ozone"
)

func (eng *Engine) RerouteAccountEventToOzone(c context.Context, e *comatproto.SyncSubscribeRepos_Account) error {
	comment := "[automod]: Account status event"
	eng.rerouteEventToOzone(c, toolsozone.ModerationEmitEvent_Input_Event{
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

/*
func (eng *Engine) RerouteTombstoneEventToOzone(c context.Context, e *comatproto.SyncSubscribeRepos_Tombstone) error {
	comment := "[automod]: Tombstone event"
	tombstone := true
	eng.rerouteEventToOzone(c, toolsozone.ModerationEmitEvent_Input_Event{
		ModerationDefs_IdentityEvent: &toolsozone.ModerationDefs_IdentityEvent{
			Comment: &comment,
			// @TODO: These don't seem to exist in the Identity event?
			// Handle:  e.Handle,
			// PdsHost:   &e.PdsHost,
			Tombstone: &tombstone,
			Timestamp: e.Time,
		},
	}, toolsozone.ModerationEmitEvent_Input_Subject{
		AdminDefs_RepoRef: &comatproto.AdminDefs_RepoRef{
			Did: e.Did,
		},
	})
	return nil
}
*/

func (eng *Engine) RerouteRecordOpToOzone(c *RecordContext) error {
	comment := "[automod]: Record event"

	eng.rerouteEventToOzone(c.Ctx, toolsozone.ModerationEmitEvent_Input_Event{
		ModerationDefs_RecordEvent: &toolsozone.ModerationDefs_RecordEvent{
			Comment:   &comment,
			Op:        c.RecordOp.Action,
			Timestamp: time.Now().Format(time.RFC3339),
		},
	}, toolsozone.ModerationEmitEvent_Input_Subject{
		RepoStrongRef: &comatproto.RepoStrongRef{
			LexiconTypeID: "com.atproto.repo.strongRef",
			Uri:           c.RecordOp.ATURI().String(),
			Cid:           c.RecordOp.CID.String(),
		},
	})

	return nil
}

func (eng *Engine) RerouteIdentityEventToOzone(c context.Context, e *comatproto.SyncSubscribeRepos_Identity) error {
	comment := "[automod]: Identity event"
	tombstone := false

	eng.rerouteEventToOzone(c, toolsozone.ModerationEmitEvent_Input_Event{
		ModerationDefs_IdentityEvent: &toolsozone.ModerationDefs_IdentityEvent{
			Comment: &comment,
			Handle:  e.Handle,
			// @TODO: This doesn't seem to exist in the Identity event?
			// PdsHost:   &e.PdsHost,
			Tombstone: &tombstone,
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
func (eng *Engine) IsDuplicatingEvent(ctx context.Context, event toolsozone.ModerationEmitEvent_Input_Event, subject toolsozone.ModerationEmitEvent_Input_Subject) (bool, error) {
	if eng.OzoneClient == nil {
		eng.Logger.Warn("can not check if event is duplicate, mod service client not configured")
		return false, nil
	}

	eventType := ""
	if event.ModerationDefs_AccountEvent != nil {
		eventType = event.ModerationDefs_AccountEvent.LexiconTypeID
	} else if event.ModerationDefs_IdentityEvent != nil {
		eventType = event.ModerationDefs_IdentityEvent.LexiconTypeID
	} else if event.ModerationDefs_RecordEvent != nil {
		eventType = event.ModerationDefs_RecordEvent.LexiconTypeID
	}

	eventSubject := ""
	if subject.AdminDefs_RepoRef != nil {
		eventSubject = subject.AdminDefs_RepoRef.Did
	} else if subject.RepoStrongRef != nil {
		eventSubject = subject.RepoStrongRef.Uri
	}

	xrpcc := eng.OzoneClient
	resp, err := toolsozone.ModerationQueryEvents(
		ctx,
		xrpcc,
		nil, // addedLabels []string
		nil, // addedTags []string
		nil, // collections []string
		"",  // comment string
		time.Now().Add(-time.Minute*5).Format(time.RFC3339), // createdAfter string
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
		return true, nil
	}

	return false, nil
}

func (eng *Engine) rerouteEventToOzone(ctx context.Context, event toolsozone.ModerationEmitEvent_Input_Event, subject toolsozone.ModerationEmitEvent_Input_Subject) error {
	// if we can't actually talk to service, bail out early
	if eng.OzoneClient == nil {
		eng.Logger.Warn("not persisting ozone account event, mod service client not configured")
		return nil
	}

	isDuplicate, duplicateCheckError := eng.IsDuplicatingEvent(ctx, event, subject)
	if duplicateCheckError != nil {
		eng.Logger.Error("failed to check if event is duplicate", "err", duplicateCheckError)
		return duplicateCheckError
	}

	if isDuplicate {
		eng.Logger.Info("event was already emitted, not emitting again")
		return nil
	}

	xrpcc := eng.OzoneClient
	_, err := toolsozone.ModerationEmitEvent(ctx, xrpcc, &toolsozone.ModerationEmitEvent_Input{
		CreatedBy: xrpcc.Auth.Did,
		Event:     &event,
		Subject:   &subject,
	})
	if err != nil {
		eng.Logger.Error("failed to re route event to ozone", "err", err)
		return err
	}

	return nil
}
