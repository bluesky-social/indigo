package relay

import (
	"context"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/cmd/relay/relay/models"
	"github.com/bluesky-social/indigo/cmd/relay/stream"
	"github.com/bluesky-social/indigo/cmd/relay/stream/eventmgr"
	"github.com/bluesky-social/indigo/util/cliutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingPersister struct {
	events []*stream.XRPCStreamEvent
}

func (p *recordingPersister) Persist(ctx context.Context, e *stream.XRPCStreamEvent) error {
	p.events = append(p.events, e)
	return nil
}
func (p *recordingPersister) Playback(ctx context.Context, since int64, cb func(*stream.XRPCStreamEvent) error) error {
	return nil
}
func (p *recordingPersister) TakeDownRepo(ctx context.Context, uid uint64) error { return nil }
func (p *recordingPersister) Flush(ctx context.Context) error                    { return nil }
func (p *recordingPersister) Shutdown(ctx context.Context) error                 { return nil }
func (p *recordingPersister) SetEventBroadcaster(func(*stream.XRPCStreamEvent))  {}

func testRelayWithAccount(t *testing.T, did syntax.DID, upstream models.AccountStatus) (*Relay, *MockHostChecker, *recordingPersister) {
	t.Helper()

	db, err := cliutil.SetupDatabase("sqlite://:memory:", 1)
	require.NoError(t, err)

	dir := identity.NewMockDirectory()
	dir.Insert(identity.Identity{
		DID:    did,
		Handle: syntax.Handle("account.example.com"),
		Services: map[string]identity.ServiceEndpoint{
			"atproto_pds": {Type: "AtprotoPersonalDataServer", URL: "https://pds.example.com"},
		},
	})

	p := &recordingPersister{}
	r, err := NewRelay(db, eventmgr.NewEventManager(p), dir, DefaultRelayConfig())
	require.NoError(t, err)

	hc := NewMockHostChecker()
	r.HostChecker = hc

	require.NoError(t, db.Create(&models.Account{
		DID:            did.String(),
		HostID:         1,
		Status:         models.AccountStatusActive,
		UpstreamStatus: upstream,
	}).Error)

	return r, hc, p
}

func TestEnsureAccountActiveEmitsAccountEvent(t *testing.T) {
	ctx := context.Background()
	did := syntax.DID("did:plc:ensureactivetest")

	deactivated := string(models.AccountStatusDeactivated)
	takendown := string(models.AccountStatusTakendown)

	tests := []struct {
		name          string
		hostStatus    models.AccountStatus
		wantErr       bool
		wantEvent     bool
		wantActive    bool
		wantStatusVal *string
	}{
		{name: "reactivated upstream", hostStatus: models.AccountStatusActive, wantEvent: true, wantActive: true},
		{name: "different inactive status upstream", hostStatus: models.AccountStatusTakendown, wantErr: true, wantEvent: true, wantStatusVal: &takendown},
		{name: "unchanged inactive status upstream", hostStatus: models.AccountStatusDeactivated, wantErr: true, wantStatusVal: &deactivated},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, hc, p := testRelayWithAccount(t, did, models.AccountStatusDeactivated)
			hc.Accounts[did.String()] = tc.hostStatus

			acc, err := r.GetAccount(ctx, did)
			require.NoError(t, err)

			err = r.EnsureAccountActive(ctx, acc)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			stored, err := r.GetAccount(ctx, did)
			require.NoError(t, err)
			assert.Equal(t, tc.hostStatus, stored.UpstreamStatus)

			if !tc.wantEvent {
				assert.Empty(t, p.events)
				return
			}
			require.Len(t, p.events, 1)
			evt := p.events[0].RepoAccount
			require.NotNil(t, evt)
			assert.Equal(t, did.String(), evt.Did)
			assert.Equal(t, tc.wantActive, evt.Active)
			assert.Equal(t, tc.wantStatusVal, evt.Status)
			assert.Equal(t, acc.UID, p.events[0].PrivUid)
		})
	}
}

func TestEnsureAccountActiveNoEventWhenAlreadyActive(t *testing.T) {
	ctx := context.Background()
	did := syntax.DID("did:plc:ensureactivetest")

	r, _, p := testRelayWithAccount(t, did, models.AccountStatusActive)
	acc, err := r.GetAccount(ctx, did)
	require.NoError(t, err)

	// the mock host checker has no entry for this account, so any upstream lookup would fail
	assert.NoError(t, r.EnsureAccountActive(ctx, acc))
	assert.Empty(t, p.events)
}
