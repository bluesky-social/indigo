package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/cmd/relay/relay"
	"github.com/bluesky-social/indigo/cmd/relay/relay/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAccountLimitAlertClaimer struct {
	claims    []relay.HostAccountLimitAlertClaims
	lastLimit int
	calls     int
	records   []string
}

func (f *fakeAccountLimitAlertClaimer) ClaimDueAccountLimitAlerts(ctx context.Context, threshold float64, repeatInterval time.Duration, limit int) (*relay.HostAccountLimitAlertClaims, error) {
	f.lastLimit = limit
	if len(f.claims) == 0 {
		return &relay.HostAccountLimitAlertClaims{}, nil
	}
	idx := f.calls
	if idx >= len(f.claims) {
		idx = len(f.claims) - 1
	}
	f.calls++
	return &f.claims[idx], nil
}

func (f *fakeAccountLimitAlertClaimer) RecordHostAccountLimitAlertSent(ctx context.Context, hostname string, state models.HostAccountLimitAlertState, sentAt time.Time) error {
	f.records = append(f.records, hostname+":"+string(state))
	return nil
}

type recordingAlerter struct {
	messages []AlertMessage
}

func (r *recordingAlerter) SendAlert(ctx context.Context, msg AlertMessage) error {
	r.messages = append(r.messages, msg)
	return nil
}

func TestAccountLimitAlertMonitorSendsClaimedWarning(t *testing.T) {
	claimer := &fakeAccountLimitAlertClaimer{
		claims: []relay.HostAccountLimitAlertClaims{
			{
				Warnings: []relay.HostAccountLimitUsage{
					{ID: 1, Hostname: "pds.example.com", AccountCount: 80, AccountLimit: 100, Usage: 0.8, AlertSentAt: time.Unix(1000, 0)},
				},
			},
		},
	}
	alerter := &recordingAlerter{}
	config := DefaultAccountLimitAlertMonitorConfig()
	config.CheckInterval = time.Minute
	config.RepeatInterval = time.Hour
	config.Environment = "test"

	monitor, err := NewAccountLimitAlertMonitor(slog.Default(), claimer, alerter, config)
	require.NoError(t, err)

	require.NoError(t, monitor.Check(context.Background()))
	require.Len(t, alerter.messages, 1)
	assert.Contains(t, alerter.messages[0].Title, "in test")
	assert.Contains(t, alerter.messages[0].Text, "`pds.example.com`: *80.0%* used (`80 / 100` repos)")
}

func TestJitteredCheckInterval(t *testing.T) {
	interval := 5 * time.Minute
	jitter := time.Minute
	rangeSize := int64(2*jitter + 1)

	assert.Equal(t, 4*time.Minute, jitteredCheckInterval(interval, jitter, func(n int64) int64 {
		assert.Equal(t, rangeSize, n)
		return 0
	}))
	assert.Equal(t, interval, jitteredCheckInterval(interval, jitter, func(n int64) int64 {
		assert.Equal(t, rangeSize, n)
		return int64(jitter)
	}))
	assert.Equal(t, 6*time.Minute, jitteredCheckInterval(interval, jitter, func(n int64) int64 {
		assert.Equal(t, rangeSize, n)
		return int64(2 * jitter)
	}))
	assert.Equal(t, time.Second, jitteredCheckInterval(30*time.Second, jitter, func(n int64) int64 {
		return 0
	}))
	assert.Equal(t, interval, jitteredCheckInterval(interval, 0, nil))
}

func TestInitialCheckDelay(t *testing.T) {
	jitter := time.Minute
	rangeSize := int64(jitter + 1)

	assert.Equal(t, time.Duration(0), initialCheckDelay(jitter, func(n int64) int64 {
		assert.Equal(t, rangeSize, n)
		return 0
	}))
	assert.Equal(t, jitter, initialCheckDelay(jitter, func(n int64) int64 {
		assert.Equal(t, rangeSize, n)
		return int64(jitter)
	}))
	assert.Equal(t, time.Duration(0), initialCheckDelay(0, nil))
}

func TestAccountLimitAlertMonitorCirculatesLocalSentState(t *testing.T) {
	claimer := &fakeAccountLimitAlertClaimer{
		claims: []relay.HostAccountLimitAlertClaims{
			{
				Warnings: []relay.HostAccountLimitUsage{
					{ID: 1, Hostname: "pds.example.com", AccountCount: 80, AccountLimit: 100, Usage: 0.8, AlertSentAt: time.Unix(1000, 0)},
				},
			},
		},
	}
	alerter := &recordingAlerter{}
	var sent []AccountLimitAlertSentState
	config := DefaultAccountLimitAlertMonitorConfig()
	config.SentCallback = func(ctx context.Context, state AccountLimitAlertSentState) {
		sent = append(sent, state)
	}

	monitor, err := NewAccountLimitAlertMonitor(slog.Default(), claimer, alerter, config)
	require.NoError(t, err)
	require.NoError(t, monitor.Check(context.Background()))

	require.Len(t, sent, 1)
	assert.Equal(t, AccountLimitAlertKindWarning, sent[0].Kind)
	assert.Equal(t, uint64(1), sent[0].HostID)
	assert.Equal(t, "pds.example.com", sent[0].Hostname)
}

func TestAccountLimitAlertMonitorSendsClaimedRecovery(t *testing.T) {
	claimer := &fakeAccountLimitAlertClaimer{
		claims: []relay.HostAccountLimitAlertClaims{
			{
				Recoveries: []relay.HostAccountLimitUsage{
					{ID: 1, Hostname: "pds.example.com", AccountCount: 20, AccountLimit: 100, Usage: 0.2, AlertSentAt: time.Unix(1000, 0)},
				},
			},
		},
	}
	alerter := &recordingAlerter{}
	config := DefaultAccountLimitAlertMonitorConfig()
	config.RepeatInterval = time.Hour

	monitor, err := NewAccountLimitAlertMonitor(slog.Default(), claimer, alerter, config)
	require.NoError(t, err)

	require.NoError(t, monitor.Check(context.Background()))
	require.Len(t, alerter.messages, 1)
	assert.Contains(t, alerter.messages[0].Title, "PDS repo limit recovered")
	assert.Contains(t, alerter.messages[0].Text, "`pds.example.com` is no longer above the 80.0% repo-limit alert threshold")
}

func TestAccountLimitAlertMonitorBatchesMessagesWithoutQueryCap(t *testing.T) {
	claimer := &fakeAccountLimitAlertClaimer{
		claims: []relay.HostAccountLimitAlertClaims{
			{
				Warnings: []relay.HostAccountLimitUsage{
					{ID: 1, Hostname: "one.example.com", AccountCount: 80, AccountLimit: 100, Usage: 0.8, AlertSentAt: time.Unix(1000, 0)},
					{ID: 2, Hostname: "two.example.com", AccountCount: 81, AccountLimit: 100, Usage: 0.81, AlertSentAt: time.Unix(1000, 0)},
				},
			},
		},
	}
	alerter := &recordingAlerter{}
	config := DefaultAccountLimitAlertMonitorConfig()
	config.HostsPerMessage = 1

	monitor, err := NewAccountLimitAlertMonitor(slog.Default(), claimer, alerter, config)
	require.NoError(t, err)
	require.NoError(t, monitor.Check(context.Background()))

	assert.Equal(t, 0, claimer.lastLimit)
	require.Len(t, alerter.messages, 2)
	assert.Contains(t, alerter.messages[0].Text, "one.example.com")
	assert.Contains(t, alerter.messages[1].Text, "two.example.com")
}

func TestAccountLimitAlertMonitorBatchesMessagesBySlackTextLimit(t *testing.T) {
	var warnings []relay.HostAccountLimitUsage
	for i := 0; i < 80; i++ {
		hostname := strings.Repeat("long-hostname-", 12) + fmt.Sprintf("%02d.example.com", i)
		warnings = append(warnings, relay.HostAccountLimitUsage{
			ID:           uint64(i + 1),
			Hostname:     hostname,
			AccountCount: 90,
			AccountLimit: 100,
			Usage:        0.9,
			AlertSentAt:  time.Unix(1000, 0),
		})
	}
	claimer := &fakeAccountLimitAlertClaimer{
		claims: []relay.HostAccountLimitAlertClaims{
			{Warnings: warnings},
		},
	}
	alerter := &recordingAlerter{}
	config := DefaultAccountLimitAlertMonitorConfig()

	monitor, err := NewAccountLimitAlertMonitor(slog.Default(), claimer, alerter, config)
	require.NoError(t, err)
	require.NoError(t, monitor.Check(context.Background()))

	require.Greater(t, len(alerter.messages), 1)
	seenHosts := 0
	for _, msg := range alerter.messages {
		assert.LessOrEqual(t, len([]rune(msg.Text)), accountLimitAlertMaxTextChars)
		seenHosts += strings.Count(msg.Text, ".example.com")
	}
	assert.Equal(t, len(warnings), seenHosts)
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

	require.NoError(t, alerter.SendAlert(context.Background(), AlertMessage{
		Title:    "PDS repo limit warning in test",
		Text:     "`pds.example.com`: *85.0%* used (`85 / 100` repos)",
		Severity: AlertSeverityWarning,
		Fields: []AlertField{
			{Title: "Threshold", Value: "80.0%"},
			{Title: "Highest usage", Value: "85.0%"},
		},
		Actions: []AlertAction{
			{Text: "Open relay dashboard", URL: "https://relay.example.com/dash"},
		},
	}))
	assert.Equal(t, "Bearer "+token, gotAuth)

	payloadBytes, err := json.Marshal(gotPayload)
	require.NoError(t, err)
	assert.False(t, strings.Contains(string(payloadBytes), token))
	assert.Equal(t, "C123", gotPayload["channel"])
	assert.Contains(t, gotPayload["text"], "PDS repo limit warning in test")

	attachments, ok := gotPayload["attachments"].([]any)
	require.True(t, ok)
	require.Len(t, attachments, 1)
	attachment, ok := attachments[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "#ECB22E", attachment["color"])
	blocks, ok := attachment["blocks"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, blocks)

	payload := string(payloadBytes)
	assert.Contains(t, payload, "Open relay dashboard")
	assert.Contains(t, payload, "https://relay.example.com/dash")
}
