package main

import (
	"testing"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/cmd/tap/models"
)

func newTestFirehoseProcessor(te *testEnv, fullNetwork bool) *FirehoseProcessor {
	config := &TapConfig{
		RelayUrl:                   "wss://relay.test.example",
		FullNetworkMode:            fullNetwork,
		FirehoseParallelism:        1,
		FirehoseCursorSaveInterval: 5 * time.Second,
		EventCacheSize:             1000,
	}
	return NewFirehoseProcessor(te.server.logger, te.db, te.events, te.repos, config)
}

// --- ProcessCommit skip path tests ---

func TestProcessCommit_UntrackedRepo(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{})
	fp := newTestFirehoseProcessor(te, false)

	evt := &comatproto.SyncSubscribeRepos_Commit{
		Repo: "did:example:unknown",
		Rev:  "3jzfcijpj2z2a",
		Seq:  1,
	}

	err := fp.ProcessCommit(te.ctx, evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify no repo was created
	var count int64
	te.db.Model(&models.Repo{}).Where("did = ?", "did:example:unknown").Count(&count)
	if count != 0 {
		t.Fatal("expected no repo to be created for untracked DID")
	}
}

func TestProcessCommit_UntrackedRepo_FullNetworkAutoTrack(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{})
	fp := newTestFirehoseProcessor(te, true)

	evt := &comatproto.SyncSubscribeRepos_Commit{
		Repo: "did:example:autotrack",
		Rev:  "3jzfcijpj2z2a",
		Seq:  1,
	}

	err := fp.ProcessCommit(te.ctx, evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify repo was auto-created with pending state
	var repo models.Repo
	if err := te.db.First(&repo, "did = ?", "did:example:autotrack").Error; err != nil {
		t.Fatalf("expected repo to be auto-created: %v", err)
	}
	if repo.State != models.RepoStatePending {
		t.Fatalf("expected state=pending, got %s", repo.State)
	}
}

func TestProcessCommit_SkipsWrongState(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{})
	fp := newTestFirehoseProcessor(te, false)

	did := "did:example:pending-repo"
	te.insertRepo(did, models.RepoStatePending, "", "", "")

	evt := &comatproto.SyncSubscribeRepos_Commit{
		Repo: did,
		Rev:  "3jzfcijpj2z2a",
		Seq:  1,
	}

	err := fp.ProcessCommit(te.ctx, evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Repo state should be unchanged
	var repo models.Repo
	te.db.First(&repo, "did = ?", did)
	if repo.State != models.RepoStatePending {
		t.Fatalf("expected state=pending (unchanged), got %s", repo.State)
	}
}

func TestProcessCommit_SkipsReplayedRev(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{})
	fp := newTestFirehoseProcessor(te, false)

	did := "did:example:replayed-rev"
	rev := "3jzfcijpj2z2a"
	te.insertRepo(did, models.RepoStateActive, rev, "bafyreie5cvv4h45feadgeuwhbcutmh6t2ceseocckahdoe6uat64zmz454", "")

	evt := &comatproto.SyncSubscribeRepos_Commit{
		Repo: did,
		Rev:  rev, // same as current rev
		Seq:  1,
	}

	err := fp.ProcessCommit(te.ctx, evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No state changes should have occurred
	var repo models.Repo
	te.db.First(&repo, "did = ?", did)
	if repo.Rev != rev {
		t.Fatalf("expected rev unchanged at %s, got %s", rev, repo.Rev)
	}
}

// --- ProcessAccount tests ---

func TestProcessAccount_Deactivation(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{})
	fp := newTestFirehoseProcessor(te, false)

	did := "did:example:deactivated"
	te.insertRepo(did, models.RepoStateActive, "3jzfcijpj2z2a", "bafyreie5cvv4h45feadgeuwhbcutmh6t2ceseocckahdoe6uat64zmz454", "alice.test")

	status := string(models.AccountStatusDeactivated)
	evt := &comatproto.SyncSubscribeRepos_Account{
		Did:    did,
		Active: false,
		Status: &status,
		Seq:    1,
	}

	if err := fp.ProcessAccount(te.ctx, evt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var repo models.Repo
	te.db.First(&repo, "did = ?", did)
	if repo.Status != models.AccountStatusDeactivated {
		t.Fatalf("expected status=deactivated, got %s", repo.Status)
	}

	// Verify identity event was emitted
	var bufCount int64
	te.db.Model(&models.OutboxBuffer{}).Where("did = ?", did).Count(&bufCount)
	if bufCount != 1 {
		t.Fatalf("expected 1 outbox event, got %d", bufCount)
	}
}

func TestProcessAccount_Reactivation(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{})
	fp := newTestFirehoseProcessor(te, false)

	did := "did:example:reactivated"
	te.insertRepo(did, models.RepoStateActive, "3jzfcijpj2z2a", "bafyreie5cvv4h45feadgeuwhbcutmh6t2ceseocckahdoe6uat64zmz454", "bob.test")
	// Set status to deactivated
	te.db.Model(&models.Repo{}).Where("did = ?", did).Update("status", models.AccountStatusDeactivated)

	evt := &comatproto.SyncSubscribeRepos_Account{
		Did:    did,
		Active: true,
		Seq:    1,
	}

	if err := fp.ProcessAccount(te.ctx, evt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var repo models.Repo
	te.db.First(&repo, "did = ?", did)
	if repo.Status != models.AccountStatusActive {
		t.Fatalf("expected status=active, got %s", repo.Status)
	}

	// Verify identity event was emitted
	var bufCount int64
	te.db.Model(&models.OutboxBuffer{}).Where("did = ?", did).Count(&bufCount)
	if bufCount != 1 {
		t.Fatalf("expected 1 outbox event, got %d", bufCount)
	}
}

func TestProcessAccount_Deletion(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{})
	fp := newTestFirehoseProcessor(te, false)

	did := "did:example:deleted"
	te.insertRepo(did, models.RepoStateActive, "3jzfcijpj2z2a", "bafyreie5cvv4h45feadgeuwhbcutmh6t2ceseocckahdoe6uat64zmz454", "charlie.test")

	// Add some records and resync buffer entries
	te.db.Create(&models.RepoRecord{Did: did, Collection: "app.bsky.feed.post", Rkey: "3jzfcijpj2z3a", Cid: "bafyreibj4lsc3aqnrvphp5xmrnfoorvru4wynt6lwidqbm2623a6tatzdu"})
	te.db.Create(&models.ResyncBuffer{Did: did, Data: `{"did":"` + did + `"}`})

	status := string(models.AccountStatusDeleted)
	evt := &comatproto.SyncSubscribeRepos_Account{
		Did:    did,
		Active: false,
		Status: &status,
		Seq:    1,
	}

	if err := fp.ProcessAccount(te.ctx, evt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Repo should be deleted
	var repoCount int64
	te.db.Model(&models.Repo{}).Where("did = ?", did).Count(&repoCount)
	if repoCount != 0 {
		t.Fatal("expected repo to be deleted")
	}

	// Records should be deleted
	var recCount int64
	te.db.Model(&models.RepoRecord{}).Where("did = ?", did).Count(&recCount)
	if recCount != 0 {
		t.Fatal("expected records to be deleted")
	}

	// Resync buffer should be cleared
	var bufCount int64
	te.db.Model(&models.ResyncBuffer{}).Where("did = ?", did).Count(&bufCount)
	if bufCount != 0 {
		t.Fatal("expected resync buffer to be cleared")
	}

	// Identity event should have been emitted
	var outboxCount int64
	te.db.Model(&models.OutboxBuffer{}).Where("did = ?", did).Count(&outboxCount)
	if outboxCount != 1 {
		t.Fatalf("expected 1 outbox event, got %d", outboxCount)
	}
}

func TestProcessAccount_NoOpSameStatus(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{})
	fp := newTestFirehoseProcessor(te, false)

	did := "did:example:same-status"
	te.insertRepo(did, models.RepoStateActive, "3jzfcijpj2z2a", "bafyreie5cvv4h45feadgeuwhbcutmh6t2ceseocckahdoe6uat64zmz454", "dana.test")

	evt := &comatproto.SyncSubscribeRepos_Account{
		Did:    did,
		Active: true,
		Seq:    1,
	}

	if err := fp.ProcessAccount(te.ctx, evt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No outbox event should have been created
	var bufCount int64
	te.db.Model(&models.OutboxBuffer{}).Where("did = ?", did).Count(&bufCount)
	if bufCount != 0 {
		t.Fatalf("expected no outbox events for same status, got %d", bufCount)
	}
}

func TestProcessAccount_UntrackedRepo(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{})
	fp := newTestFirehoseProcessor(te, false)

	did := "did:example:untracked-acct"
	status := string(models.AccountStatusDeactivated)
	evt := &comatproto.SyncSubscribeRepos_Account{
		Did:    did,
		Active: false,
		Status: &status,
		Seq:    1,
	}

	if err := fp.ProcessAccount(te.ctx, evt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Nothing should be created
	var count int64
	te.db.Model(&models.Repo{}).Where("did = ?", did).Count(&count)
	if count != 0 {
		t.Fatal("expected no repo to be created for untracked DID")
	}
}

func TestProcessAccount_FullNetworkAutoTrack(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{})
	fp := newTestFirehoseProcessor(te, true)

	did := "did:example:fullnet-acct"
	evt := &comatproto.SyncSubscribeRepos_Account{
		Did:    did,
		Active: true,
		Seq:    1,
	}

	if err := fp.ProcessAccount(te.ctx, evt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var repo models.Repo
	if err := te.db.First(&repo, "did = ?", did).Error; err != nil {
		t.Fatalf("expected repo to be auto-created: %v", err)
	}
	if repo.State != models.RepoStatePending {
		t.Fatalf("expected state=pending, got %s", repo.State)
	}
}

// --- ProcessIdentity tests ---

func TestProcessIdentity_HandleChange(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{})
	fp := newTestFirehoseProcessor(te, false)

	did := "did:example:handle-change"
	te.insertRepo(did, models.RepoStateActive, "3jzfcijpj2z2a", "bafyreie5cvv4h45feadgeuwhbcutmh6t2ceseocckahdoe6uat64zmz454", "old-handle.test")

	// Set up mock directory with new handle
	te.idDir.Insert(identity.Identity{
		DID:    syntax.DID(did),
		Handle: syntax.Handle("new-handle.test"),
	})

	handle := "new-handle.test"
	evt := &comatproto.SyncSubscribeRepos_Identity{
		Did:    did,
		Handle: &handle,
		Seq:    1,
	}

	if err := fp.ProcessIdentity(te.ctx, evt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Handle should be updated in DB
	var repo models.Repo
	te.db.First(&repo, "did = ?", did)
	if repo.Handle != "new-handle.test" {
		t.Fatalf("expected handle=new-handle.test, got %s", repo.Handle)
	}

	// Identity event should be emitted
	var bufCount int64
	te.db.Model(&models.OutboxBuffer{}).Where("did = ?", did).Count(&bufCount)
	if bufCount != 1 {
		t.Fatalf("expected 1 outbox event, got %d", bufCount)
	}
}

func TestProcessIdentity_NoChange(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{})
	fp := newTestFirehoseProcessor(te, false)

	did := "did:example:same-handle"
	te.insertRepo(did, models.RepoStateActive, "3jzfcijpj2z2a", "bafyreie5cvv4h45feadgeuwhbcutmh6t2ceseocckahdoe6uat64zmz454", "same-handle.test")

	te.idDir.Insert(identity.Identity{
		DID:    syntax.DID(did),
		Handle: syntax.Handle("same-handle.test"),
	})

	handle := "same-handle.test"
	evt := &comatproto.SyncSubscribeRepos_Identity{
		Did:    did,
		Handle: &handle,
		Seq:    1,
	}

	if err := fp.ProcessIdentity(te.ctx, evt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No identity event should be emitted
	var bufCount int64
	te.db.Model(&models.OutboxBuffer{}).Where("did = ?", did).Count(&bufCount)
	if bufCount != 0 {
		t.Fatalf("expected no outbox events for same handle, got %d", bufCount)
	}
}

func TestProcessIdentity_UntrackedRepo(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{})
	fp := newTestFirehoseProcessor(te, false)

	did := "did:example:untracked-id"
	handle := "untracked.test"
	evt := &comatproto.SyncSubscribeRepos_Identity{
		Did:    did,
		Handle: &handle,
		Seq:    1,
	}

	if err := fp.ProcessIdentity(te.ctx, evt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No repo should be created
	var count int64
	te.db.Model(&models.Repo{}).Where("did = ?", did).Count(&count)
	if count != 0 {
		t.Fatal("expected no repo to be created for untracked DID")
	}
}

func TestProcessIdentity_FullNetworkAutoTrack(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{})
	fp := newTestFirehoseProcessor(te, true)

	did := "did:example:fullnet-id"
	handle := "fullnet.test"
	evt := &comatproto.SyncSubscribeRepos_Identity{
		Did:    did,
		Handle: &handle,
		Seq:    1,
	}

	if err := fp.ProcessIdentity(te.ctx, evt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var repo models.Repo
	if err := te.db.First(&repo, "did = ?", did).Error; err != nil {
		t.Fatalf("expected repo to be auto-created: %v", err)
	}
	if repo.State != models.RepoStatePending {
		t.Fatalf("expected state=pending, got %s", repo.State)
	}
}

func TestProcessAccount_UnknownStatus(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{})
	fp := newTestFirehoseProcessor(te, false)

	did := "did:example:throttled"
	te.insertRepo(did, models.RepoStateActive, "3jzfcijpj2z2a", "bafyreie5cvv4h45feadgeuwhbcutmh6t2ceseocckahdoe6uat64zmz454", "throttled.test")

	status := "throttled" // not in the recognized status list
	evt := &comatproto.SyncSubscribeRepos_Account{
		Did:    did,
		Active: false,
		Status: &status,
		Seq:    1,
	}

	if err := fp.ProcessAccount(te.ctx, evt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No outbox event should have been created — unknown status is a no-op
	var bufCount int64
	te.db.Model(&models.OutboxBuffer{}).Where("did = ?", did).Count(&bufCount)
	if bufCount != 0 {
		t.Fatalf("expected no outbox events for unknown status, got %d", bufCount)
	}

	// Status should remain active
	var repo models.Repo
	te.db.First(&repo, "did = ?", did)
	if repo.Status != models.AccountStatusActive {
		t.Fatalf("expected status unchanged at active, got %s", repo.Status)
	}
}

func TestProcessAccount_FullNetworkInactiveSkipped(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{})
	fp := newTestFirehoseProcessor(te, true)

	did := "did:example:fullnet-inactive"
	status := string(models.AccountStatusDeactivated)
	evt := &comatproto.SyncSubscribeRepos_Account{
		Did:    did,
		Active: false,
		Status: &status,
		Seq:    1,
	}

	if err := fp.ProcessAccount(te.ctx, evt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Even in full network mode, inactive accounts should NOT be auto-tracked
	var count int64
	te.db.Model(&models.Repo{}).Where("did = ?", did).Count(&count)
	if count != 0 {
		t.Fatal("expected no repo to be created for inactive untracked account")
	}
}

// --- Cursor management tests ---

func TestGetCursor_NoPriorSave(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{})
	fp := newTestFirehoseProcessor(te, false)

	cursor, err := fp.GetCursor(te.ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cursor != 0 {
		t.Fatalf("expected cursor=0 with no prior save, got %d", cursor)
	}
}

func TestCursorSaveAndLoad(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{})
	fp := newTestFirehoseProcessor(te, false)

	// Store a seq and save cursor
	fp.lastSeq.Store(12345)
	if err := fp.saveCursor(te.ctx); err != nil {
		t.Fatalf("failed to save cursor: %v", err)
	}

	// Create a new processor and load cursor
	fp2 := newTestFirehoseProcessor(te, false)
	cursor, err := fp2.GetCursor(te.ctx)
	if err != nil {
		t.Fatalf("failed to get cursor: %v", err)
	}
	if cursor != 12345 {
		t.Fatalf("expected cursor=12345, got %d", cursor)
	}
}
