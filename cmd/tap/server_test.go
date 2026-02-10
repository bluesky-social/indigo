package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

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

	dids := []string{"did:plc:repo1", "did:plc:repo2", "did:plc:repo3"}

	// Add repos
	payload := fmt.Sprintf(`{"dids":["%s","%s","%s"]}`, dids[0], dids[1], dids[2])
	resp, err := http.Post(te.baseURL()+"/repos/add", "application/json", strings.NewReader(payload))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Verify repo count
	resp, err = http.Get(te.baseURL() + "/stats/repo-count")
	require.NoError(t, err)
	defer resp.Body.Close()

	var countBody map[string]int64
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&countBody))
	assert.Equal(t, int64(3), countBody["repo_count"])

	// Remove one repo
	removePayload := fmt.Sprintf(`{"dids":["%s"]}`, dids[0])
	resp2, err := http.Post(te.baseURL()+"/repos/remove", "application/json", strings.NewReader(removePayload))
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)

	// Verify repo count decreased
	resp3, err := http.Get(te.baseURL() + "/stats/repo-count")
	require.NoError(t, err)
	defer resp3.Body.Close()

	var countBody2 map[string]int64
	require.NoError(t, json.NewDecoder(resp3.Body).Decode(&countBody2))
	assert.Equal(t, int64(2), countBody2["repo_count"])
}

func TestStatsEndpoints(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{
		outboxMode: OutboxModeFireAndForget,
	})

	endpoints := []struct {
		path string
		key  string
	}{
		{"/stats/repo-count", "repo_count"},
		{"/stats/record-count", "record_count"},
		{"/stats/outbox-buffer", "outbox_buffer"},
		{"/stats/resync-buffer", "resync_buffer"},
	}

	for _, ep := range endpoints {
		t.Run(ep.path, func(t *testing.T) {
			resp, err := http.Get(te.baseURL() + ep.path)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode)

			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

			var result map[string]int64
			require.NoError(t, json.Unmarshal(body, &result))
			_, exists := result[ep.key]
			assert.True(t, exists, "response should contain key %q", ep.key)
		})
	}
}
