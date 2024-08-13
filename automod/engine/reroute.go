package engine

import (
	"context"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	toolsozone "github.com/bluesky-social/indigo/api/ozone"
)

func (eng *Engine) RerouteAccountEventToOzone(c context.Context, e *comatproto.SyncSubscribeRepos_Account) error {
	comment := "[automod]: Account status event"
	eng.RerouteEventToOzone(c, toolsozone.ModerationEmitEvent_Input_Event{
		ModerationDefs_AccountEvent: &toolsozone.ModerationDefs_AccountEvent{
			Comment:   &comment,
			Timestamp: e.Time,
			Status:    *e.Status,
			Active:    &e.Active,
		},
	}, toolsozone.ModerationEmitEvent_Input_Subject{
		AdminDefs_RepoRef: &comatproto.AdminDefs_RepoRef{
			Did: e.Did,
		},
	})
	return nil
}

func (eng *Engine) RerouteTombstoneEventToOzone(c context.Context, e *comatproto.SyncSubscribeRepos_Tombstone) error {
	comment := "[automod]: Tombstone event"
	tombstone := true
	eng.RerouteEventToOzone(c, toolsozone.ModerationEmitEvent_Input_Event{
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

func (eng *Engine) RerouteRecordOpToOzone(c context.Context, e *RecordOp) error {
	comment := "[automod]: Record event"

	eng.RerouteEventToOzone(c, toolsozone.ModerationEmitEvent_Input_Event{
		ModerationDefs_RecordEvent: &toolsozone.ModerationDefs_RecordEvent{
			Comment:   &comment,
			Op:        e.Action,
			Timestamp: time.Now().Format(time.RFC3339),
		},
	}, toolsozone.ModerationEmitEvent_Input_Subject{
		RepoStrongRef: &comatproto.RepoStrongRef{
			LexiconTypeID: "com.atproto.repo.strongRef",
			Uri:           e.ATURI().String(),
			Cid:           e.CID.String(),
		},
	})

	return nil
}

func (eng *Engine) RerouteIdentityEventToOzone(c context.Context, e *comatproto.SyncSubscribeRepos_Identity) error {
	comment := "[automod]: Identity event"
	tombstone := false

	eng.RerouteEventToOzone(c, toolsozone.ModerationEmitEvent_Input_Event{
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

func (eng *Engine) RerouteEventToOzone(ctx context.Context, event toolsozone.ModerationEmitEvent_Input_Event, subject toolsozone.ModerationEmitEvent_Input_Subject) error {
	// if we can't actually talk to service, bail out early
	if eng.OzoneClient == nil {
		eng.Logger.Warn("not persisting ozone account event, mod service client not configured")
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
