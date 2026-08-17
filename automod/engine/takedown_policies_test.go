package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	toolsozone "github.com/bluesky-social/indigo/api/ozone"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod/countstore"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/stretchr/testify/assert"
)

// captures every tools.ozone.moderation.emitEvent input received by the mock Ozone server
type emitEventCapture struct {
	mu     sync.Mutex
	inputs []toolsozone.ModerationEmitEvent_Input
}

func (capture *emitEventCapture) all() []toolsozone.ModerationEmitEvent_Input {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	return append([]toolsozone.ModerationEmitEvent_Input{}, capture.inputs...)
}

func (capture *emitEventCapture) takedowns() []toolsozone.ModerationEmitEvent_Input {
	var out []toolsozone.ModerationEmitEvent_Input
	for _, input := range capture.all() {
		if input.Event != nil && input.Event.ModerationDefs_ModEventTakedown != nil {
			out = append(out, input)
		}
	}
	return out
}

// engine fixture with an Ozone client pointed at a mock server which records emitEvent inputs
func takedownPoliciesEngineFixture(t *testing.T) (Engine, *emitEventCapture) {
	capture := &emitEventCapture{}
	mux := http.NewServeMux()
	mux.HandleFunc("/xrpc/tools.ozone.moderation.emitEvent", func(w http.ResponseWriter, r *http.Request) {
		var input toolsozone.ModerationEmitEvent_Input
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		capture.mu.Lock()
		capture.inputs = append(capture.inputs, input)
		capture.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	eng := EngineTestFixture()
	eng.OzoneClient = &xrpc.Client{
		Client: srv.Client(),
		Host:   srv.URL,
		Auth:   &xrpc.AuthInfo{Did: "did:plc:automod"},
	}
	return eng, capture
}

func takedownPoliciesAccountContext(ctx context.Context, eng *Engine) AccountContext {
	ident := identity.Identity{
		DID:    syntax.DID("did:plc:abc111"),
		Handle: syntax.Handle("handle.example.com"),
	}
	return NewAccountContext(ctx, eng, AccountMeta{Identity: &ident})
}

func takedownPoliciesRecordContext(ctx context.Context, eng *Engine) RecordContext {
	ident := identity.Identity{
		DID:    syntax.DID("did:plc:abc111"),
		Handle: syntax.Handle("handle.example.com"),
	}
	cid := syntax.CID("cid123")
	op := RecordOp{
		Action:     CreateOp,
		DID:        ident.DID,
		Collection: syntax.NSID("app.bsky.feed.post"),
		RecordKey:  syntax.RecordKey("abc123"),
		CID:        &cid,
		RecordCBOR: []byte{},
	}
	return NewRecordContext(ctx, eng, AccountMeta{Identity: &ident}, op)
}

func TestTakedownPoliciesUnattributed(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()

	// account-level, no-arg call
	eng, capture := takedownPoliciesEngineFixture(t)
	ac := takedownPoliciesAccountContext(ctx, &eng)
	ac.TakedownAccount()
	assert.NoError(eng.persistAccountModActions(&ac))
	takedowns := capture.takedowns()
	if assert.Len(takedowns, 1) {
		assert.NotNil(takedowns[0].Subject.AdminDefs_RepoRef)
		assert.Empty(takedowns[0].Event.ModerationDefs_ModEventTakedown.Policies)
	}

	// record-level, no-arg call
	eng, capture = takedownPoliciesEngineFixture(t)
	rc := takedownPoliciesRecordContext(ctx, &eng)
	rc.TakedownRecord()
	assert.NoError(eng.persistRecordModActions(&rc))
	takedowns = capture.takedowns()
	if assert.Len(takedowns, 1) {
		assert.NotNil(takedowns[0].Subject.RepoStrongRef)
		assert.Empty(takedowns[0].Event.ModerationDefs_ModEventTakedown.Policies)
	}
}

func TestTakedownPoliciesSinglePolicy(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()

	// account-level
	eng, capture := takedownPoliciesEngineFixture(t)
	ac := takedownPoliciesAccountContext(ctx, &eng)
	ac.TakedownAccount("policy-csam")
	assert.NoError(eng.persistAccountModActions(&ac))
	takedowns := capture.takedowns()
	if assert.Len(takedowns, 1) {
		assert.NotNil(takedowns[0].Subject.AdminDefs_RepoRef)
		assert.Equal([]string{"policy-csam"}, takedowns[0].Event.ModerationDefs_ModEventTakedown.Policies)
	}

	// record-level, with blob takedowns unchanged
	eng, capture = takedownPoliciesEngineFixture(t)
	rc := takedownPoliciesRecordContext(ctx, &eng)
	rc.TakedownRecord("policy-csam")
	rc.TakedownBlob("blobcid1")
	rc.TakedownBlob("blobcid1")
	rc.TakedownBlob("blobcid2")
	assert.NoError(eng.persistRecordModActions(&rc))
	takedowns = capture.takedowns()
	if assert.Len(takedowns, 1) {
		assert.NotNil(takedowns[0].Subject.RepoStrongRef)
		assert.Equal([]string{"policy-csam"}, takedowns[0].Event.ModerationDefs_ModEventTakedown.Policies)
		assert.Equal([]string{"blobcid1", "blobcid2"}, takedowns[0].SubjectBlobCids)
	}
}

func TestTakedownPoliciesDedupeStableOrder(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()

	// account-level: multiple calls, overlapping policies
	eng, capture := takedownPoliciesEngineFixture(t)
	ac := takedownPoliciesAccountContext(ctx, &eng)
	ac.TakedownAccount("policy-b")
	ac.TakedownAccount("policy-a", "policy-b")
	ac.TakedownAccount("policy-c", "policy-a")
	assert.NoError(eng.persistAccountModActions(&ac))
	takedowns := capture.takedowns()
	if assert.Len(takedowns, 1) {
		assert.Equal([]string{"policy-b", "policy-a", "policy-c"}, takedowns[0].Event.ModerationDefs_ModEventTakedown.Policies)
	}

	// record-level
	eng, capture = takedownPoliciesEngineFixture(t)
	rc := takedownPoliciesRecordContext(ctx, &eng)
	rc.TakedownRecord("policy-b")
	rc.TakedownRecord("policy-a", "policy-b")
	rc.TakedownRecord("policy-c", "policy-a")
	assert.NoError(eng.persistRecordModActions(&rc))
	takedowns = capture.takedowns()
	if assert.Len(takedowns, 1) {
		assert.Equal([]string{"policy-b", "policy-a", "policy-c"}, takedowns[0].Event.ModerationDefs_ModEventTakedown.Policies)
	}
}

func TestTakedownPoliciesFiveDistinctAccepted(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()

	five := []string{"policy-1", "policy-2", "policy-3", "policy-4", "policy-5"}

	eng, capture := takedownPoliciesEngineFixture(t)
	ac := takedownPoliciesAccountContext(ctx, &eng)
	ac.TakedownAccount(five...)
	assert.NoError(eng.persistAccountModActions(&ac))
	takedowns := capture.takedowns()
	if assert.Len(takedowns, 1) {
		assert.Equal(five, takedowns[0].Event.ModerationDefs_ModEventTakedown.Policies)
	}

	eng, capture = takedownPoliciesEngineFixture(t)
	rc := takedownPoliciesRecordContext(ctx, &eng)
	rc.TakedownRecord(five...)
	assert.NoError(eng.persistRecordModActions(&rc))
	takedowns = capture.takedowns()
	if assert.Len(takedowns, 1) {
		assert.Equal(five, takedowns[0].Event.ModerationDefs_ModEventTakedown.Policies)
	}
}

func TestTakedownPoliciesSixDistinctError(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()

	six := []string{"policy-1", "policy-2", "policy-3", "policy-4", "policy-5", "policy-6"}

	// account-level: error raised before quota consumption or any Ozone request
	eng, capture := takedownPoliciesEngineFixture(t)
	ac := takedownPoliciesAccountContext(ctx, &eng)
	// duplicates don't count toward the limit; six distinct values do
	ac.TakedownAccount(six...)
	ac.TakedownAccount("policy-1")
	assert.Error(eng.persistAccountModActions(&ac))
	assert.Empty(capture.all())
	quota, err := eng.Counters.GetCount(ctx, "automod-quota", "takedown", countstore.PeriodDay)
	assert.NoError(err)
	assert.Equal(0, quota)

	// record-level: record union validated before delegating to account-level persistence
	eng, capture = takedownPoliciesEngineFixture(t)
	rc := takedownPoliciesRecordContext(ctx, &eng)
	rc.TakedownRecord(six...)
	assert.Error(eng.persistRecordModActions(&rc))
	assert.Empty(capture.all())
	quota, err = eng.Counters.GetCount(ctx, "automod-quota", "takedown", countstore.PeriodDay)
	assert.NoError(err)
	assert.Equal(0, quota)

	// record path with oversized account union: no account-side events emitted either
	eng, capture = takedownPoliciesEngineFixture(t)
	rc = takedownPoliciesRecordContext(ctx, &eng)
	rc.TakedownAccount(six...)
	rc.TakedownRecord("policy-1")
	assert.Error(eng.persistRecordModActions(&rc))
	assert.Empty(capture.all())
	quota, err = eng.Counters.GetCount(ctx, "automod-quota", "takedown", countstore.PeriodDay)
	assert.NoError(err)
	assert.Equal(0, quota)
}

func TestTakedownPoliciesWithoutTakedownFlagNotEmitted(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()

	// policies accumulated without their takedown flag are never validated or emitted,
	// even when the union is oversized; another action still persists normally
	eng, capture := takedownPoliciesEngineFixture(t)
	ac := takedownPoliciesAccountContext(ctx, &eng)
	ac.effects.AccountTakedownPolicies = []string{"policy-1", "policy-2", "policy-3", "policy-4", "policy-5", "policy-6"}
	ac.AddAccountLabel("test-label")
	assert.NoError(eng.persistAccountModActions(&ac))
	assert.Empty(capture.takedowns())
	assert.Len(capture.all(), 1)

	eng, capture = takedownPoliciesEngineFixture(t)
	rc := takedownPoliciesRecordContext(ctx, &eng)
	rc.effects.RecordTakedownPolicies = []string{"policy-1", "policy-2", "policy-3", "policy-4", "policy-5", "policy-6"}
	rc.AddRecordLabel("test-label")
	assert.NoError(eng.persistRecordModActions(&rc))
	assert.Empty(capture.takedowns())
	assert.Len(capture.all(), 1)
}

func TestTakedownPoliciesConcurrentCalls(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()

	eng, capture := takedownPoliciesEngineFixture(t)
	rc := takedownPoliciesRecordContext(ctx, &eng)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rc.TakedownRecord(fmt.Sprintf("policy-%d", i%3))
			rc.TakedownAccount(fmt.Sprintf("policy-%d", i%3))
		}(i)
	}
	wg.Wait()

	assert.True(rc.effects.RecordTakedown)
	assert.True(rc.effects.AccountTakedown)
	assert.Len(rc.effects.RecordTakedownPolicies, 20)
	assert.Len(rc.effects.AccountTakedownPolicies, 20)

	assert.NoError(eng.persistRecordModActions(&rc))
	takedowns := capture.takedowns()
	if assert.Len(takedowns, 2) {
		for _, td := range takedowns {
			assert.ElementsMatch([]string{"policy-0", "policy-1", "policy-2"}, td.Event.ModerationDefs_ModEventTakedown.Policies)
		}
	}
}
