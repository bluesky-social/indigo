package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/cmd/relay/relay"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAccountLimitUsageLister struct {
	usages    []relay.HostAccountLimitUsage
	lastLimit int
}

func (f *fakeAccountLimitUsageLister) ListHostsApproachingAccountLimit(ctx context.Context, threshold float64, limit int) ([]relay.HostAccountLimitUsage, error) {
	f.lastLimit = limit
	return f.usages, nil
}

type recordingAlerter struct {
	messages []AlertMessage
}

func (r *recordingAlerter) SendAlert(ctx context.Context, msg AlertMessage) error {
	r.messages = append(r.messages, msg)
	return nil
}

func TestAccountLimitAlertMonitorSendsAndRepeatsAfterCooldown(t *testing.T) {
	lister := &fakeAccountLimitUsageLister{
		usages: []relay.HostAccountLimitUsage{
			{ID: 1, Hostname: "pds.example.com", AccountCount: 80, AccountLimit: 100, Usage: 0.8},
		},
	}
	alerter := &recordingAlerter{}
	config := DefaultAccountLimitAlertMonitorConfig()
	config.CheckInterval = time.Minute
	config.RepeatInterval = time.Hour
	config.Environment = "test"

	monitor, err := NewAccountLimitAlertMonitor(slog.Default(), lister, alerter, config)
	require.NoError(t, err)

	now := time.Unix(1000, 0)
	monitor.now = func() time.Time { return now }

	require.NoError(t, monitor.Check(context.Background()))
	require.Len(t, alerter.messages, 1)
	assert.Contains(t, alerter.messages[0].Text, "pds.example.com: 80.0% used (80/100 repos)")
	assert.Contains(t, alerter.messages[0].Text, "in test")

	now = now.Add(30 * time.Minute)
	require.NoError(t, monitor.Check(context.Background()))
	require.Len(t, alerter.messages, 1)

	now = now.Add(31 * time.Minute)
	require.NoError(t, monitor.Check(context.Background()))
	require.Len(t, alerter.messages, 2)
}

func TestAccountLimitAlertMonitorSendsRecoveryAndAlertsAgain(t *testing.T) {
	lister := &fakeAccountLimitUsageLister{
		usages: []relay.HostAccountLimitUsage{
			{ID: 1, Hostname: "pds.example.com", AccountCount: 80, AccountLimit: 100, Usage: 0.8},
		},
	}
	alerter := &recordingAlerter{}
	config := DefaultAccountLimitAlertMonitorConfig()
	config.RepeatInterval = time.Hour

	monitor, err := NewAccountLimitAlertMonitor(slog.Default(), lister, alerter, config)
	require.NoError(t, err)
	now := time.Unix(1000, 0)
	monitor.now = func() time.Time { return now }

	require.NoError(t, monitor.Check(context.Background()))
	require.Len(t, alerter.messages, 1)

	lister.usages = nil
	now = now.Add(10 * time.Minute)
	require.NoError(t, monitor.Check(context.Background()))
	require.Len(t, alerter.messages, 2)
	assert.Contains(t, alerter.messages[1].Text, "Relay PDS repo limit recovered")
	assert.Contains(t, alerter.messages[1].Text, "pds.example.com is no longer above the 80.0% repo-limit alert threshold")

	lister.usages = []relay.HostAccountLimitUsage{
		{ID: 1, Hostname: "pds.example.com", AccountCount: 81, AccountLimit: 100, Usage: 0.81},
	}
	now = now.Add(10 * time.Minute)
	require.NoError(t, monitor.Check(context.Background()))
	require.Len(t, alerter.messages, 3)
	assert.Contains(t, alerter.messages[2].Text, "pds.example.com: 81.0% used (81/100 repos)")
}

func TestAccountLimitAlertMonitorBatchesMessagesWithoutQueryCap(t *testing.T) {
	lister := &fakeAccountLimitUsageLister{
		usages: []relay.HostAccountLimitUsage{
			{ID: 1, Hostname: "one.example.com", AccountCount: 80, AccountLimit: 100, Usage: 0.8},
			{ID: 2, Hostname: "two.example.com", AccountCount: 81, AccountLimit: 100, Usage: 0.81},
		},
	}
	alerter := &recordingAlerter{}
	config := DefaultAccountLimitAlertMonitorConfig()
	config.HostsPerMessage = 1

	monitor, err := NewAccountLimitAlertMonitor(slog.Default(), lister, alerter, config)
	require.NoError(t, err)
	require.NoError(t, monitor.Check(context.Background()))

	assert.Equal(t, 0, lister.lastLimit)
	require.Len(t, alerter.messages, 2)
	assert.Contains(t, alerter.messages[0].Text, "one.example.com")
	assert.Contains(t, alerter.messages[1].Text, "two.example.com")
}

func TestSlackAlerterDoesNotPutTokenInPayload(t *testing.T) {
	const token = "xoxb-secret-token"
	var gotAuth string
	var gotPayload map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotPayload))
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	alerter, err := NewSlackAlerter(token, "C123")
	require.NoError(t, err)
	alerter.endpoint = server.URL
	alerter.client = server.Client()

	require.NoError(t, alerter.SendAlert(context.Background(), AlertMessage{Text: "hello"}))
	assert.Equal(t, "Bearer "+token, gotAuth)

	payloadBytes, err := json.Marshal(gotPayload)
	require.NoError(t, err)
	assert.False(t, strings.Contains(string(payloadBytes), token))
	assert.Equal(t, "C123", gotPayload["channel"])
	assert.Equal(t, "hello", gotPayload["text"])
	assert.Equal(t, false, gotPayload["mrkdwn"])
}
