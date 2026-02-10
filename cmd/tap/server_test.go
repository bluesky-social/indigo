package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

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
