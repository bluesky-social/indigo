package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/cmd/tap/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthcheck(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{
		outboxMode: OutboxModeFireAndForget,
	})

	resp, err := http.Get(te.baseURL() + "/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "ok", body["status"])
}

func TestAddAndRemoveRepos(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{
		outboxMode: OutboxModeFireAndForget,
	})

	dids := []string{"did:example:repo1", "did:example:repo2", "did:example:repo3"}

	// Add repos
	payload := fmt.Sprintf(`{"dids":["%s","%s","%s"]}`, dids[0], dids[1], dids[2])
	resp, err := http.Post(te.baseURL()+"/repos/add", "application/json", strings.NewReader(payload))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Verify repos exist in DB
	var count int64
	te.db.Model(&models.Repo{}).Count(&count)
	assert.Equal(t, int64(3), count)

	// Remove one repo
	removePayload := fmt.Sprintf(`{"dids":["%s"]}`, dids[0])
	resp2, err := http.Post(te.baseURL()+"/repos/remove", "application/json", strings.NewReader(removePayload))
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)

	// Verify count decreased
	te.db.Model(&models.Repo{}).Count(&count)
	assert.Equal(t, int64(2), count)
}

func TestStatsRepoCount(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{
		outboxMode: OutboxModeFireAndForget,
	})

	// Insert repos directly
	te.db.Create(&models.Repo{Did: "did:example:stats1", State: models.RepoStateActive})
	te.db.Create(&models.Repo{Did: "did:example:stats2", State: models.RepoStatePending})

	resp, err := http.Get(te.baseURL() + "/stats/repo-count")
	require.NoError(t, err)
	defer resp.Body.Close()

	var body map[string]int64
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, int64(2), body["repo_count"])
}

func TestStatsRecordCount(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{
		outboxMode: OutboxModeFireAndForget,
	})

	// Insert records directly
	te.db.Create(&models.RepoRecord{Did: "did:example:rec", Collection: "app.bsky.feed.post", Rkey: "1", Cid: "cid1"})
	te.db.Create(&models.RepoRecord{Did: "did:example:rec", Collection: "app.bsky.feed.post", Rkey: "2", Cid: "cid2"})
	te.db.Create(&models.RepoRecord{Did: "did:example:rec", Collection: "app.bsky.feed.like", Rkey: "1", Cid: "cid3"})

	resp, err := http.Get(te.baseURL() + "/stats/record-count")
	require.NoError(t, err)
	defer resp.Body.Close()

	var body map[string]int64
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, int64(3), body["record_count"])
}

func TestStatsOutboxBuffer(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{
		outboxMode: OutboxModeFireAndForget,
	})

	// Push events — they'll be written to the outbox_buffers table
	te.pushRecordEvents("did:example:outbox", 4, false)

	resp, err := http.Get(te.baseURL() + "/stats/outbox-buffer")
	require.NoError(t, err)
	defer resp.Body.Close()

	var body map[string]int64
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, int64(4), body["outbox_buffer"])
}

func TestStatsResyncBuffer(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{
		outboxMode: OutboxModeFireAndForget,
	})

	// Insert resync buffer entries directly
	te.db.Create(&models.ResyncBuffer{Did: "did:example:resync1", Data: `{}`})
	te.db.Create(&models.ResyncBuffer{Did: "did:example:resync2", Data: `{}`})

	resp, err := http.Get(te.baseURL() + "/stats/resync-buffer")
	require.NoError(t, err)
	defer resp.Body.Close()

	var body map[string]int64
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, int64(2), body["resync_buffer"])
}

type repoInfoResp struct {
	Did        string `json:"did"`
	State      string `json:"state"`
	Backfilled bool   `json:"backfilled"`
}

func getRepoInfo(t *testing.T, url string) (int, repoInfoResp) {
	t.Helper()
	resp, err := http.Get(url)
	require.NoError(t, err)
	defer resp.Body.Close()

	var body repoInfoResp
	if resp.StatusCode == http.StatusOK {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	}
	return resp.StatusCode, body
}

func TestInfoRepo_Backfilled(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{
		outboxMode: OutboxModeWebsocketAck,
	})

	did := "did:example:infobackfill"
	te.insertRepo(did, models.RepoStateActive, "rev1", "", "info.test")

	consumer, err := newTestConsumer(te.wsURL())
	require.NoError(t, err)
	defer consumer.close()

	te.pushRecordEvents(did, 2, false)
	msgs := consumer.waitForMessages(2, time.Second)
	require.Len(t, msgs, 2)

	status, info := getRepoInfo(t, te.baseURL()+"/info/"+did)
	require.Equal(t, http.StatusOK, status)
	assert.False(t, info.Backfilled, "historical events are still unacked")

	for _, msg := range msgs {
		require.NoError(t, consumer.sendAck(msg.ID))
	}

	require.Eventually(t, func() bool {
		_, info := getRepoInfo(t, te.baseURL()+"/info/"+did)
		return info.Backfilled
	}, time.Second, 10*time.Millisecond)
}

func TestWaitRepo_ReturnsOnceHistoricalEventsAcked(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{
		outboxMode: OutboxModeWebsocketAck,
	})

	did := "did:example:waitacked"
	te.insertRepo(did, models.RepoStateActive, "rev1", "", "wait.test")

	consumer, err := newTestConsumer(te.wsURL())
	require.NoError(t, err)
	defer consumer.close()

	// two record events plus an identity event, all historical
	te.pushRecordEvents(did, 2, false)
	te.pushIdentityEvent(did, "wait.test", models.AccountStatusActive)
	msgs := consumer.waitForMessages(3, time.Second)
	require.Len(t, msgs, 3)

	type result struct {
		status  int
		info    repoInfoResp
		elapsed time.Duration
	}
	done := make(chan result, 1)
	start := time.Now()
	go func() {
		status, info := getRepoInfo(t, te.baseURL()+"/repos/wait/"+did+"?timeout=5s")
		done <- result{status, info, time.Since(start)}
	}()

	select {
	case <-done:
		t.Fatal("wait returned before the historical events were acked")
	case <-time.After(300 * time.Millisecond):
	}

	for _, msg := range msgs {
		require.NoError(t, consumer.sendAck(msg.ID))
	}

	select {
	case res := <-done:
		require.Equal(t, http.StatusOK, res.status)
		assert.True(t, res.info.Backfilled)
		assert.Equal(t, string(models.RepoStateActive), res.info.State)
		assert.Less(t, res.elapsed, 2*time.Second, "wait should return soon after the last ack, not at the timeout")
	case <-time.After(3 * time.Second):
		t.Fatal("wait did not return after all historical events were acked")
	}
}

func TestWaitRepo_IgnoresLiveEvents(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{
		outboxMode: OutboxModeWebsocketAck,
	})

	did := "did:example:waitlive"
	te.insertRepo(did, models.RepoStateActive, "rev1", "", "live.test")

	// unacked live events should not hold up a backfill wait
	te.pushRecordEvents(did, 2, true)

	start := time.Now()
	status, info := getRepoInfo(t, te.baseURL()+"/repos/wait/"+did+"?timeout=5s")
	require.Equal(t, http.StatusOK, status)
	assert.True(t, info.Backfilled)
	assert.Less(t, time.Since(start), time.Second)
}

func TestWaitRepo_TimesOutWhileRepoIsPending(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{
		outboxMode: OutboxModeFireAndForget,
	})

	did := "did:example:waitpending"
	te.insertRepo(did, models.RepoStatePending, "", "", "")

	start := time.Now()
	status, info := getRepoInfo(t, te.baseURL()+"/repos/wait/"+did+"?timeout=300ms")
	elapsed := time.Since(start)

	require.Equal(t, http.StatusOK, status)
	assert.False(t, info.Backfilled)
	assert.Equal(t, string(models.RepoStatePending), info.State)
	assert.GreaterOrEqual(t, elapsed, 300*time.Millisecond)
	assert.Less(t, elapsed, 2*time.Second)
}

func TestWaitRepo_ReturnsWhenRepoBecomesActive(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{
		outboxMode: OutboxModeFireAndForget,
	})

	did := "did:example:waitactivate"
	te.insertRepo(did, models.RepoStateResyncing, "", "", "")

	go func() {
		time.Sleep(200 * time.Millisecond)
		te.db.Model(&models.Repo{}).Where("did = ?", did).Update("state", models.RepoStateActive)
	}()

	start := time.Now()
	status, info := getRepoInfo(t, te.baseURL()+"/repos/wait/"+did+"?timeout=5s")
	require.Equal(t, http.StatusOK, status)
	assert.True(t, info.Backfilled)
	assert.Less(t, time.Since(start), 2*time.Second)
}

func TestWaitRepo_ErrorStateReturnsEarly(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{
		outboxMode: OutboxModeFireAndForget,
	})

	did := "did:example:waiterror"
	te.insertRepo(did, models.RepoStateError, "", "", "")

	start := time.Now()
	status, info := getRepoInfo(t, te.baseURL()+"/repos/wait/"+did+"?timeout=5s")
	require.Equal(t, http.StatusOK, status)
	assert.False(t, info.Backfilled)
	assert.Equal(t, string(models.RepoStateError), info.State)
	assert.Less(t, time.Since(start), time.Second)
}

func TestWaitRepo_BadRequests(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{
		outboxMode: OutboxModeFireAndForget,
	})

	status, _ := getRepoInfo(t, te.baseURL()+"/repos/wait/did:example:nobody?timeout=1s")
	assert.Equal(t, http.StatusNotFound, status)

	te.insertRepo("did:example:waitbad", models.RepoStateActive, "rev1", "", "")
	for _, timeout := range []string{"soon", "-1s", "0s"} {
		status, _ := getRepoInfo(t, te.baseURL()+"/repos/wait/did:example:waitbad?timeout="+timeout)
		assert.Equal(t, http.StatusBadRequest, status, "timeout=%s", timeout)
	}
}
