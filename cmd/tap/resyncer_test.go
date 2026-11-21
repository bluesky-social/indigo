package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/carstore"
	"github.com/bluesky-social/indigo/cmd/tap/models"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/repo"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/ipld/go-car"
	"gorm.io/gorm"
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

	for i := range 4 {
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

// --- doResync tests ---

// newResyncTestPDS returns a mock PDS that serves the repo CAR currently
// stored in carData for com.atproto.sync.getRepo requests.
func newResyncTestPDS(t *testing.T, carData *atomic.Value) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/xrpc/com.atproto.sync.getRepo" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		data, _ := carData.Load().([]byte)
		if len(data) == 0 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.ipld.car")
		w.Write(data)
	}))
}

func newResyncTestRepo(t *testing.T, ctx context.Context, did string) (*repo.Repo, blockstore.Blockstore) {
	t.Helper()
	bs := blockstore.NewBlockstore(datastore.NewMapDatastore())
	return repo.NewRepo(ctx, did, bs), bs
}

func resyncTestCommit(t *testing.T, ctx context.Context, r *repo.Repo, priv *atcrypto.PrivateKeyK256) (cid.Cid, string) {
	t.Helper()
	signer := func(_ context.Context, _ string, msg []byte) ([]byte, error) {
		return priv.HashAndSign(msg)
	}
	commitCID, rev, err := r.Commit(ctx, signer)
	if err != nil {
		t.Fatalf("failed to commit repo: %v", err)
	}
	return commitCID, rev
}

func resyncTestCAR(t *testing.T, ctx context.Context, bs blockstore.Blockstore, root cid.Cid) []byte {
	t.Helper()
	var buf bytes.Buffer
	h := &car.CarHeader{Roots: []cid.Cid{root}, Version: 1}
	hb, err := cbor.DumpObject(h)
	if err != nil {
		t.Fatalf("failed to encode CAR header: %v", err)
	}
	if _, err := carstore.LdWrite(&buf, hb); err != nil {
		t.Fatalf("failed to write CAR header: %v", err)
	}
	kc, err := bs.AllKeysChan(ctx)
	if err != nil {
		t.Fatalf("failed to list blockstore keys: %v", err)
	}
	for k := range kc {
		blk, err := bs.Get(ctx, k)
		if err != nil {
			t.Fatalf("failed to get block %s: %v", k, err)
		}
		// blockstores may key blocks by their raw-codec CID; repo CARs reference
		// the same content by its dag-cbor CID
		dagCID := cid.NewCidV1(cid.DagCBOR, k.Hash())
		if _, err := carstore.LdWrite(&buf, dagCID.Bytes(), blk.RawData()); err != nil {
			t.Fatalf("failed to write CAR block: %v", err)
		}
	}
	return buf.Bytes()
}

func resyncTestPost(text string) *appbsky.FeedPost {
	return &appbsky.FeedPost{
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Text:      text,
	}
}

func resyncTestOpCreate(path string, c cid.Cid) *comatproto.SyncSubscribeRepos_RepoOp {
	l := lexutil.LexLink(c)
	return &comatproto.SyncSubscribeRepos_RepoOp{Action: "create", Path: path, Cid: &l}
}

func resyncTestOpUpdate(path string, prev, c cid.Cid) *comatproto.SyncSubscribeRepos_RepoOp {
	p := lexutil.LexLink(prev)
	l := lexutil.LexLink(c)
	return &comatproto.SyncSubscribeRepos_RepoOp{Action: "update", Path: path, Cid: &l, Prev: &p}
}

func resyncTestCommitEvent(did string, seq int64, commitCID cid.Cid, rev string, carBytes []byte, ops []*comatproto.SyncSubscribeRepos_RepoOp, prevData cid.Cid) *comatproto.SyncSubscribeRepos_Commit {
	evt := &comatproto.SyncSubscribeRepos_Commit{
		Blobs:  []lexutil.LexLink{},
		Blocks: lexutil.LexBytes(carBytes),
		Commit: lexutil.LexLink(commitCID),
		Ops:    ops,
		Repo:   did,
		Rev:    rev,
		Seq:    seq,
		Time:   time.Now().UTC().Format(time.RFC3339),
	}
	if prevData.Defined() {
		pd := lexutil.LexLink(prevData)
		evt.PrevData = &pd
	}
	return evt
}

func resyncTestRecordEvts(t *testing.T, te *testEnv, did, rev string) []*RecordEvt {
	t.Helper()
	var rows []models.OutboxBuffer
	if err := te.db.Find(&rows, "did = ?", did).Error; err != nil {
		t.Fatalf("failed to load outbox rows: %v", err)
	}
	var evts []*RecordEvt
	for _, row := range rows {
		var m MarshallableEvt
		if err := json.Unmarshal([]byte(row.Data), &m); err != nil {
			t.Fatalf("failed to unmarshal outbox event: %v", err)
		}
		if m.Type == "record" && m.RecordEvt != nil && m.RecordEvt.Rev == rev {
			evts = append(evts, m.RecordEvt)
		}
	}
	return evts
}

// TestResyncEmitsDeletesForRemovedRecords reproduces bluesky-social/indigo#1561:
// a repo is backfilled, then records are deleted and others created while Tap
// misses the intermediate commits. When a later commit triggers a resync, the
// resync must emit delete events for records that no longer exist in the repo.
func TestResyncEmitsDeletesForRemovedRecords(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{})
	r := newTestResyncer(te)
	// allow requests to the local mock PDS; production keeps the SSRF-guarded transport
	r.repoFetchClient = &http.Client{Timeout: 30 * time.Second}

	ctx := te.ctx
	did := "did:example:resync-missing-deletes"
	collection := "app.bsky.feed.post"

	priv, err := atcrypto.GeneratePrivateKeyK256()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	pub, err := priv.PublicKey()
	if err != nil {
		t.Fatalf("failed to derive public key: %v", err)
	}

	var carData atomic.Value
	pds := newResyncTestPDS(t, &carData)
	defer pds.Close()

	te.idDir.Insert(identity.Identity{
		DID:    syntax.DID(did),
		Handle: syntax.Handle("resync.test"),
		Services: map[string]identity.ServiceEndpoint{
			"atproto_pds": {Type: "AtprotoPersonalDataServer", URL: pds.URL},
		},
		Keys: map[string]identity.VerificationMethod{
			"atproto": {Type: "Multikey", PublicKeyMultibase: pub.Multibase()},
		},
	})
	te.insertRepo(did, models.RepoStatePending, "", "", "")

	wrepo, bs := newResyncTestRepo(t, ctx, did)

	// v1: two posts, delivered via backfill resync
	_, tidA, err := wrepo.CreateRecord(ctx, collection, resyncTestPost("post A"))
	if err != nil {
		t.Fatalf("failed to create record A: %v", err)
	}
	cidB, tidB, err := wrepo.CreateRecord(ctx, collection, resyncTestPost("post B"))
	if err != nil {
		t.Fatalf("failed to create record B: %v", err)
	}
	commitCID1, _ := resyncTestCommit(t, ctx, wrepo, priv)
	carData.Store(resyncTestCAR(t, ctx, bs, commitCID1))

	if err := r.resyncDid(ctx, did); err != nil {
		t.Fatalf("backfill resync failed: %v", err)
	}

	var repoRow models.Repo
	if err := te.db.First(&repoRow, "did = ?", did).Error; err != nil {
		t.Fatalf("failed to load repo: %v", err)
	}
	if repoRow.State != models.RepoStateActive {
		t.Fatalf("expected repo state=active after backfill, got %s", repoRow.State)
	}
	var recCount int64
	te.db.Model(&models.RepoRecord{}).Where("did = ?", did).Count(&recCount)
	if recCount != 2 {
		t.Fatalf("expected 2 records after backfill, got %d", recCount)
	}

	// v2 (missed by Tap): delete A, create C
	if err := wrepo.DeleteRecord(ctx, collection+"/"+tidA); err != nil {
		t.Fatalf("failed to delete record A: %v", err)
	}
	cidC, tidC, err := wrepo.CreateRecord(ctx, collection, resyncTestPost("post C"))
	if err != nil {
		t.Fatalf("failed to create record C: %v", err)
	}
	_, _ = resyncTestCommit(t, ctx, wrepo, priv)
	v2DataCID := wrepo.DataCid()

	// v3: update B, delivered via firehose and triggering desync
	cidB2, err := wrepo.UpdateRecord(ctx, collection+"/"+tidB, resyncTestPost("post B updated"))
	if err != nil {
		t.Fatalf("failed to update record B: %v", err)
	}
	commitCID3, rev3 := resyncTestCommit(t, ctx, wrepo, priv)
	carV3 := resyncTestCAR(t, ctx, bs, commitCID3)
	carData.Store(carV3)

	fp := newTestFirehoseProcessor(te, false)
	evt := resyncTestCommitEvent(did, 3, commitCID3, rev3, carV3, []*comatproto.SyncSubscribeRepos_RepoOp{
		resyncTestOpUpdate(collection+"/"+tidB, cidB, cidB2),
	}, v2DataCID)
	if err := fp.ProcessCommit(ctx, evt); err != nil {
		t.Fatalf("failed to process commit event: %v", err)
	}

	if err := te.db.First(&repoRow, "did = ?", did).Error; err != nil {
		t.Fatalf("failed to reload repo: %v", err)
	}
	if repoRow.State != models.RepoStateDesynchronized {
		t.Fatalf("expected repo state=desynchronized after prevData mismatch, got %s", repoRow.State)
	}

	// resync against the current repo state
	if err := r.resyncDid(ctx, did); err != nil {
		t.Fatalf("resync failed: %v", err)
	}

	// the deleted record must be gone from storage
	var recA models.RepoRecord
	err = te.db.First(&recA, "did = ? AND collection = ? AND rkey = ?", did, collection, tidA).Error
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected record A to be deleted, got err=%v rec=%+v", err, recA)
	}

	// surviving records: updated B and created C
	var records []models.RepoRecord
	if err := te.db.Find(&records, "did = ?", did).Error; err != nil {
		t.Fatalf("failed to load records: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records after resync, got %d: %+v", len(records), records)
	}
	gotCids := map[string]string{}
	for _, rec := range records {
		gotCids[rec.Rkey] = rec.Cid
	}
	if gotCids[tidB] != cidB2.String() {
		t.Fatalf("expected record B cid=%s, got %s", cidB2.String(), gotCids[tidB])
	}
	if gotCids[tidC] != cidC.String() {
		t.Fatalf("expected record C cid=%s, got %s", cidC.String(), gotCids[tidC])
	}

	// resync must emit update/create/delete events for this rev
	evts := resyncTestRecordEvts(t, te, did, rev3)
	if len(evts) != 3 {
		t.Fatalf("expected 3 record events for rev %s, got %d: %+v", rev3, len(evts), evts)
	}
	byRkey := map[string]*RecordEvt{}
	for _, evt := range evts {
		if evt.Live {
			t.Fatalf("expected historical (live=false) event, got %+v", evt)
		}
		byRkey[evt.Rkey] = evt
	}
	if evt := byRkey[tidA]; evt == nil || evt.Action != "delete" || evt.Collection != collection || evt.Cid != "" || evt.Record != nil {
		t.Fatalf("expected delete event for record A, got %+v", evt)
	}
	if evt := byRkey[tidB]; evt == nil || evt.Action != "update" || evt.Cid != cidB2.String() {
		t.Fatalf("expected update event for record B, got %+v", evt)
	}
	if evt := byRkey[tidC]; evt == nil || evt.Action != "create" || evt.Cid != cidC.String() {
		t.Fatalf("expected create event for record C, got %+v", evt)
	}
}
