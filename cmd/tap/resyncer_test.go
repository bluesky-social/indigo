package main

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/cmd/tap/models"
)

func newTestResyncer(te *testEnv) *Resyncer {
	config := &TapConfig{
		ResyncParallelism: 1,
		RepoFetchTimeout:  30 * time.Second,
		EventCacheSize:    1000,
	}
	return NewResyncer(te.server.logger, te.db, te.repos, te.events, config)
}

// --- claimResyncJob tests ---

func TestClaimResyncJob_Priority(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{})
	r := newTestResyncer(te)

	te.insertRepo("did:example:pending", models.RepoStatePending, "", "", "")
	te.insertRepo("did:example:desynced", models.RepoStateDesynchronized, "", "", "")
	te.insertRepo("did:example:errored", models.RepoStateError, "", "", "")

	// First claim should get pending
	did1, found1, err := r.claimResyncJob(te.ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found1 || did1 != "did:example:pending" {
		t.Fatalf("expected pending repo first, got did=%q found=%v", did1, found1)
	}

	// Second claim should get desynchronized
	did2, found2, err := r.claimResyncJob(te.ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found2 || did2 != "did:example:desynced" {
		t.Fatalf("expected desynchronized repo second, got did=%q found=%v", did2, found2)
	}

	// Third claim should get error
	did3, found3, err := r.claimResyncJob(te.ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found3 || did3 != "did:example:errored" {
		t.Fatalf("expected error repo third, got did=%q found=%v", did3, found3)
	}
}

func TestClaimResyncJob_RetryAfterRespected(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{})
	r := newTestResyncer(te)

	did := "did:example:retry-after"
	te.insertRepo(did, models.RepoStateError, "", "", "")

	// Set retry_after to the future
	futureTs := time.Now().Add(1 * time.Hour).Unix()
	te.db.Model(&models.Repo{}).Where("did = ?", did).Update("retry_after", futureTs)

	// Should not be claimable
	_, found, err := r.claimResyncJob(te.ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("expected no job found when retry_after is in the future")
	}

	// Set retry_after to the past
	pastTs := time.Now().Add(-1 * time.Minute).Unix()
	te.db.Model(&models.Repo{}).Where("did = ?", did).Update("retry_after", pastTs)

	// Should now be claimable
	got, found, err := r.claimResyncJob(te.ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found || got != did {
		t.Fatalf("expected errored repo to be claimable, got did=%q found=%v", got, found)
	}
}

func TestClaimResyncJob_NoneAvailable(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{})
	r := newTestResyncer(te)

	// Empty DB — no repos
	_, found, err := r.claimResyncJob(te.ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("expected no job found in empty DB")
	}
}

// --- handleResyncError tests ---

func TestHandleResyncError_WithError(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{})
	r := newTestResyncer(te)

	did := "did:example:resync-err"
	te.insertRepo(did, models.RepoStateActive, "3jzfcijpj2z2a", "bafyreie5cvv4h45feadgeuwhbcutmh6t2ceseocckahdoe6uat64zmz454", "alice.test")

	err := r.handleResyncError(te.ctx, did, fmt.Errorf("something went wrong"))
	if err == nil || err.Error() != "something went wrong" {
		t.Fatalf("expected original error returned, got: %v", err)
	}

	var repo models.Repo
	te.db.First(&repo, "did = ?", did)

	if repo.State != models.RepoStateError {
		t.Fatalf("expected state=error, got %s", repo.State)
	}
	if repo.ErrorMsg != "something went wrong" {
		t.Fatalf("expected error_msg='something went wrong', got %q", repo.ErrorMsg)
	}
	if repo.RetryCount != 1 {
		t.Fatalf("expected retry_count=1, got %d", repo.RetryCount)
	}
	// retry_after should be in the future (roughly 60s from now, with jitter)
	retryAfterTime := time.Unix(repo.RetryAfter, 0)
	if retryAfterTime.Before(time.Now()) {
		t.Fatalf("expected retry_after in the future, got %v", retryAfterTime)
	}
}

func TestHandleResyncError_WithNilError(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{})
	r := newTestResyncer(te)

	did := "did:example:resync-nil"
	te.insertRepo(did, models.RepoStateActive, "3jzfcijpj2z2a", "bafyreie5cvv4h45feadgeuwhbcutmh6t2ceseocckahdoe6uat64zmz454", "")

	err := r.handleResyncError(te.ctx, did, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var repo models.Repo
	te.db.First(&repo, "did = ?", did)

	if repo.State != models.RepoStateDesynchronized {
		t.Fatalf("expected state=desynchronized, got %s", repo.State)
	}
	if repo.ErrorMsg != "" {
		t.Fatalf("expected empty error_msg, got %q", repo.ErrorMsg)
	}
}

func TestHandleResyncError_ExponentialBackoff(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{})
	r := newTestResyncer(te)

	did := "did:example:backoff"
	te.insertRepo(did, models.RepoStateActive, "", "", "")

	var prevRetryAfter int64

	for i := 0; i < 4; i++ {
		// Reset state to active so GetRepoState works for handleResyncError
		te.db.Model(&models.Repo{}).Where("did = ?", did).Update("state", models.RepoStateActive)

		r.handleResyncError(te.ctx, did, fmt.Errorf("err"))

		var repo models.Repo
		te.db.First(&repo, "did = ?", did)

		if repo.RetryCount != i+1 {
			t.Fatalf("iteration %d: expected retry_count=%d, got %d", i, i+1, repo.RetryCount)
		}

		if i > 0 && repo.RetryAfter <= prevRetryAfter {
			t.Fatalf("iteration %d: expected retry_after to increase, got %d <= %d", i, repo.RetryAfter, prevRetryAfter)
		}
		prevRetryAfter = repo.RetryAfter
	}
}

// --- resetPartiallyResynced tests ---

func TestResetPartiallyResynced(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{})
	r := newTestResyncer(te)

	te.insertRepo("did:example:resyncing-1", models.RepoStateResyncing, "", "", "")
	te.insertRepo("did:example:resyncing-2", models.RepoStateResyncing, "", "", "")
	te.insertRepo("did:example:active-repo", models.RepoStateActive, "3jzfcijpj2z2a", "bafyreie5cvv4h45feadgeuwhbcutmh6t2ceseocckahdoe6uat64zmz454", "")

	if err := r.resetPartiallyResynced(te.ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var r1, r2, r3 models.Repo
	te.db.First(&r1, "did = ?", "did:example:resyncing-1")
	te.db.First(&r2, "did = ?", "did:example:resyncing-2")
	te.db.First(&r3, "did = ?", "did:example:active-repo")

	if r1.State != models.RepoStateDesynchronized {
		t.Fatalf("expected resyncing1 state=desynchronized, got %s", r1.State)
	}
	if r2.State != models.RepoStateDesynchronized {
		t.Fatalf("expected resyncing2 state=desynchronized, got %s", r2.State)
	}
	if r3.State != models.RepoStateActive {
		t.Fatalf("expected active repo unchanged, got %s", r3.State)
	}
}

// --- drainResyncBuffer tests ---

func TestDrainResyncBuffer_Basic(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{})
	r := newTestResyncer(te)

	did := "did:example:drain-basic"
	cidA := "bafyreie5cvv4h45feadgeuwhbcutmh6t2ceseocckahdoe6uat64zmz454"
	cidB := "bafyreibj4lsc3aqnrvphp5xmrnfoorvru4wynt6lwidqbm2623a6tatzdu"
	rev1 := "3jzfcijpj2z2a"
	rev2 := "3jzfcijpj2z2b"
	recordCid := "bafyreie5737gdxlw5i64vzichcalba3z2v5n6icifvx5xytvske7mr3hpm"

	te.insertRepo(did, models.RepoStateActive, rev1, cidA, "")

	commit := Commit{
		Did:      did,
		Rev:      rev2,
		DataCid:  cidB,
		PrevData: cidA,
		Ops: []CommitOp{
			{Collection: "app.bsky.feed.post", Rkey: "3jzfcijpj2z3a", Action: "create", Cid: recordCid},
		},
	}
	commitJSON, _ := json.Marshal(commit)

	te.db.Create(&models.ResyncBuffer{
		Did:  did,
		Data: string(commitJSON),
	})

	if err := r.drainResyncBuffer(te.ctx, did); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Buffer should be empty
	var bufCount int64
	te.db.Model(&models.ResyncBuffer{}).Where("did = ?", did).Count(&bufCount)
	if bufCount != 0 {
		t.Fatalf("expected buffer to be drained, got %d entries", bufCount)
	}

	// Repo should be updated
	var repo models.Repo
	te.db.First(&repo, "did = ?", did)
	if repo.Rev != rev2 {
		t.Fatalf("expected rev=%s, got %s", rev2, repo.Rev)
	}
	if repo.PrevData != cidB {
		t.Fatalf("expected prev_data=%s, got %s", cidB, repo.PrevData)
	}

	// Record should exist
	var recCount int64
	te.db.Model(&models.RepoRecord{}).Where("did = ?", did).Count(&recCount)
	if recCount != 1 {
		t.Fatalf("expected 1 record, got %d", recCount)
	}
}

func TestDrainResyncBuffer_SkipsMismatchedPrevData(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{})
	r := newTestResyncer(te)

	did := "did:example:mismatch"
	cidA := "bafyreie5cvv4h45feadgeuwhbcutmh6t2ceseocckahdoe6uat64zmz454"
	cidX := "bafyreigdm6oz2tlydvfmq4ch7tfaicrfqiwob3ux3xsh6lv5n4va5d5rem"
	cidB := "bafyreibj4lsc3aqnrvphp5xmrnfoorvru4wynt6lwidqbm2623a6tatzdu"
	rev1 := "3jzfcijpj2z2a"
	rev2 := "3jzfcijpj2z2b"

	te.insertRepo(did, models.RepoStateActive, rev1, cidA, "")

	commit := Commit{
		Did:      did,
		Rev:      rev2,
		DataCid:  cidB,
		PrevData: cidX, // doesn't match repo's cidA
		Ops: []CommitOp{
			{Collection: "app.bsky.feed.post", Rkey: "3jzfcijpj2z3a", Action: "create", Cid: "bafyreie5737gdxlw5i64vzichcalba3z2v5n6icifvx5xytvske7mr3hpm"},
		},
	}
	commitJSON, _ := json.Marshal(commit)

	te.db.Create(&models.ResyncBuffer{
		Did:  did,
		Data: string(commitJSON),
	})

	if err := r.drainResyncBuffer(te.ctx, did); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Buffer entry should still exist (not processed)
	var bufCount int64
	te.db.Model(&models.ResyncBuffer{}).Where("did = ?", did).Count(&bufCount)
	if bufCount != 1 {
		t.Fatalf("expected buffer entry to remain, got %d", bufCount)
	}

	// Repo should be unchanged
	var repo models.Repo
	te.db.First(&repo, "did = ?", did)
	if repo.Rev != rev1 {
		t.Fatalf("expected rev unchanged at %s, got %s", rev1, repo.Rev)
	}
}

func TestDrainResyncBuffer_ChainedCommits(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{})
	r := newTestResyncer(te)

	did := "did:example:chained"
	cidA := "bafyreie5cvv4h45feadgeuwhbcutmh6t2ceseocckahdoe6uat64zmz454"
	cidB := "bafyreibj4lsc3aqnrvphp5xmrnfoorvru4wynt6lwidqbm2623a6tatzdu"
	cidC := "bafyreie5737gdxlw5i64vzichcalba3z2v5n6icifvx5xytvske7mr3hpm"
	rev1 := "3jzfcijpj2z2a"
	rev2 := "3jzfcijpj2z2b"
	rev3 := "3jzfcijpj2z2c"
	recCid1 := "bafyreigdm6oz2tlydvfmq4ch7tfaicrfqiwob3ux3xsh6lv5n4va5d5rem"
	recCid2 := "bafyreifhm3zcrp65kcjmv3ke6j3v7yio5vqxdmgpnlrji77ty6mfzselm"

	te.insertRepo(did, models.RepoStateActive, rev1, cidA, "")

	// Two chained commits: A->B, B->C
	commit1 := Commit{
		Did:      did,
		Rev:      rev2,
		DataCid:  cidB,
		PrevData: cidA,
		Ops: []CommitOp{
			{Collection: "app.bsky.feed.post", Rkey: "3jzfcijpj2z3a", Action: "create", Cid: recCid1},
		},
	}
	commit2 := Commit{
		Did:      did,
		Rev:      rev3,
		DataCid:  cidC,
		PrevData: cidB,
		Ops: []CommitOp{
			{Collection: "app.bsky.feed.post", Rkey: "3jzfcijpj2z3b", Action: "create", Cid: recCid2},
		},
	}

	c1JSON, _ := json.Marshal(commit1)
	c2JSON, _ := json.Marshal(commit2)

	te.db.Create(&models.ResyncBuffer{Did: did, Data: string(c1JSON)})
	te.db.Create(&models.ResyncBuffer{Did: did, Data: string(c2JSON)})

	if err := r.drainResyncBuffer(te.ctx, did); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var repo models.Repo
	te.db.First(&repo, "did = ?", did)

	// Both chained commits should be applied
	var bufCount int64
	te.db.Model(&models.ResyncBuffer{}).Where("did = ?", did).Count(&bufCount)

	if repo.Rev != rev3 {
		t.Fatalf("expected rev=%s after both commits, got %s", rev3, repo.Rev)
	}
	if repo.PrevData != cidC {
		t.Fatalf("expected prev_data=%s after both commits, got %s", cidC, repo.PrevData)
	}
	if bufCount != 0 {
		t.Fatalf("expected buffer to be fully drained, got %d entries", bufCount)
	}
}
