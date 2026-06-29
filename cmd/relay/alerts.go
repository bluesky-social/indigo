package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/cmd/relay/relay"
	"github.com/bluesky-social/indigo/util/cliutil"

	"github.com/labstack/echo/v4"
)

const accountLimitAlertHostsPerMessage = 50

type AccountLimitAlertKind string

const (
	AccountLimitAlertKindWarning  = AccountLimitAlertKind("warning")
	AccountLimitAlertKindRecovery = AccountLimitAlertKind("recovery")
)

type AlertMessage struct {
	Text string
}

type Alerter interface {
	SendAlert(ctx context.Context, msg AlertMessage) error
}

type accountLimitUsageLister interface {
	ListHostsApproachingAccountLimit(ctx context.Context, threshold float64, limit int) ([]relay.HostAccountLimitUsage, error)
}

type AccountLimitAlertSentState struct {
	Kind         AccountLimitAlertKind `json:"kind"`
	HostID       uint64                `json:"hostID"`
	Hostname     string                `json:"hostname"`
	AccountCount int64                 `json:"accountCount"`
	AccountLimit int64                 `json:"accountLimit"`
	Usage        float64               `json:"usage"`
	SentAt       time.Time             `json:"sentAt"`
}

func accountLimitAlertSentState(kind AccountLimitAlertKind, usage relay.HostAccountLimitUsage, sentAt time.Time) AccountLimitAlertSentState {
	return AccountLimitAlertSentState{
		Kind:         kind,
		HostID:       usage.ID,
		Hostname:     usage.Hostname,
		AccountCount: usage.AccountCount,
		AccountLimit: usage.AccountLimit,
		Usage:        usage.Usage,
		SentAt:       sentAt,
	}
}

func (s *AccountLimitAlertSentState) normalize(now time.Time) error {
	switch s.Kind {
	case AccountLimitAlertKindWarning, AccountLimitAlertKindRecovery:
	default:
		return fmt.Errorf("invalid account limit alert kind: %s", s.Kind)
	}
	if s.HostID == 0 {
		return fmt.Errorf("account limit alert host ID is required")
	}
	hostname, _, err := relay.ParseHostname(s.Hostname)
	if err != nil {
		return fmt.Errorf("invalid account limit alert hostname: %w", err)
	}
	s.Hostname = hostname
	if s.SentAt.IsZero() {
		s.SentAt = now
	}
	if s.AccountLimit < 0 {
		return fmt.Errorf("account limit alert account limit must not be negative")
	}
	if s.AccountCount < 0 {
		return fmt.Errorf("account limit alert account count must not be negative")
	}
	if s.Usage < 0 {
		return fmt.Errorf("account limit alert usage must not be negative")
	}
	return nil
}

type AccountLimitAlertMonitorConfig struct {
	Threshold       float64
	CheckInterval   time.Duration
	CheckJitter     time.Duration
	RepeatInterval  time.Duration
	Environment     string
	HostsPerMessage int
	SentCallback    func(context.Context, AccountLimitAlertSentState)
}

func DefaultAccountLimitAlertMonitorConfig() AccountLimitAlertMonitorConfig {
	return AccountLimitAlertMonitorConfig{
		Threshold:       0.80,
		CheckInterval:   5 * time.Minute,
		CheckJitter:     time.Minute,
		RepeatInterval:  6 * time.Hour,
		HostsPerMessage: accountLimitAlertHostsPerMessage,
	}
}

func (c AccountLimitAlertMonitorConfig) Validate() error {
	if c.Threshold <= 0 || c.Threshold > 1 {
		return fmt.Errorf("account limit alert threshold must be greater than 0 and less than or equal to 1")
	}
	if c.CheckInterval <= 0 {
		return fmt.Errorf("account limit alert interval must be positive")
	}
	if c.CheckJitter < 0 {
		return fmt.Errorf("account limit alert jitter must not be negative")
	}
	if c.RepeatInterval <= 0 {
		return fmt.Errorf("account limit alert repeat interval must be positive")
	}
	if c.HostsPerMessage <= 0 {
		return fmt.Errorf("account limit alert hosts per message must be positive")
	}
	return nil
}

type AccountLimitAlertMonitor struct {
	logger  *slog.Logger
	lister  accountLimitUsageLister
	alerter Alerter
	config  AccountLimitAlertMonitorConfig

	lk             sync.Mutex
	now            func() time.Time
	lastAlert      map[uint64]time.Time
	aboveThreshold map[uint64]relay.HostAccountLimitUsage
}

func NewAccountLimitAlertMonitor(logger *slog.Logger, lister accountLimitUsageLister, alerter Alerter, config AccountLimitAlertMonitorConfig) (*AccountLimitAlertMonitor, error) {
	if lister == nil {
		return nil, fmt.Errorf("account limit alert lister is required")
	}
	if alerter == nil {
		return nil, fmt.Errorf("account limit alerter is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &AccountLimitAlertMonitor{
		logger:         logger.With("system", "account-limit-alerts"),
		lister:         lister,
		alerter:        alerter,
		config:         config,
		now:            time.Now,
		lastAlert:      make(map[uint64]time.Time),
		aboveThreshold: make(map[uint64]relay.HostAccountLimitUsage),
	}, nil
}

func (m *AccountLimitAlertMonitor) Run(ctx context.Context) {
	delay := initialCheckDelay(m.config.CheckJitter, rand.Int63n)
	for {
		if !waitForAlertCheck(ctx, delay) {
			return
		}
		if err := m.Check(ctx); err != nil {
			m.logger.Error("account limit alert check failed", "err", err)
		}
		delay = jitteredCheckInterval(m.config.CheckInterval, m.config.CheckJitter, rand.Int63n)
	}
}

func waitForAlertCheck(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func initialCheckDelay(jitter time.Duration, randInt63n func(int64) int64) time.Duration {
	if jitter <= 0 {
		return 0
	}
	if randInt63n == nil {
		randInt63n = rand.Int63n
	}
	return time.Duration(randInt63n(int64(jitter) + 1))
}

func jitteredCheckInterval(interval, jitter time.Duration, randInt63n func(int64) int64) time.Duration {
	if jitter <= 0 {
		return interval
	}
	if randInt63n == nil {
		randInt63n = rand.Int63n
	}

	offset := time.Duration(randInt63n(int64(jitter)*2+1)) - jitter
	delay := interval + offset
	if delay < time.Second {
		return time.Second
	}
	return delay
}

func (m *AccountLimitAlertMonitor) Check(ctx context.Context) error {
	usages, err := m.lister.ListHostsApproachingAccountLimit(ctx, m.config.Threshold, 0)
	if err != nil {
		return err
	}

	now := m.now()
	m.lk.Lock()
	currentAbove := make(map[uint64]relay.HostAccountLimitUsage, len(usages))
	due := make([]relay.HostAccountLimitUsage, 0, len(usages))
	for _, usage := range usages {
		currentAbove[usage.ID] = usage
		last, alertedBefore := m.lastAlert[usage.ID]
		_, wasAbove := m.aboveThreshold[usage.ID]
		repeatDue := !alertedBefore || now.Sub(last) >= m.config.RepeatInterval
		if !wasAbove || repeatDue {
			due = append(due, usage)
		}
	}

	recovered := make([]relay.HostAccountLimitUsage, 0)
	for id := range m.aboveThreshold {
		if _, ok := currentAbove[id]; !ok {
			recovered = append(recovered, m.aboveThreshold[id])
		}
	}

	for id, usage := range currentAbove {
		m.aboveThreshold[id] = usage
	}
	m.lk.Unlock()

	for _, usage := range recovered {
		if err := m.alerter.SendAlert(ctx, AlertMessage{Text: formatAccountLimitRecoveryAlert(m.config, usage)}); err != nil {
			return err
		}
		if err := m.recordAccountLimitAlertSent(ctx, accountLimitAlertSentState(AccountLimitAlertKindRecovery, usage, now), true); err != nil {
			return err
		}
	}

	for start := 0; start < len(due); start += m.config.HostsPerMessage {
		end := start + m.config.HostsPerMessage
		if end > len(due) {
			end = len(due)
		}
		batch := due[start:end]
		if err := m.alerter.SendAlert(ctx, AlertMessage{Text: formatAccountLimitAlert(m.config, batch)}); err != nil {
			return err
		}
		for _, usage := range batch {
			if err := m.recordAccountLimitAlertSent(ctx, accountLimitAlertSentState(AccountLimitAlertKindWarning, usage, now), true); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *AccountLimitAlertMonitor) RecordAccountLimitAlertSent(state AccountLimitAlertSentState) error {
	return m.recordAccountLimitAlertSent(context.Background(), state, false)
}

func (m *AccountLimitAlertMonitor) recordAccountLimitAlertSent(ctx context.Context, state AccountLimitAlertSentState, notifySiblings bool) error {
	now := m.now()
	if err := state.normalize(now); err != nil {
		return err
	}

	usage := relay.HostAccountLimitUsage{
		ID:           state.HostID,
		Hostname:     state.Hostname,
		AccountCount: state.AccountCount,
		AccountLimit: state.AccountLimit,
		Usage:        state.Usage,
	}

	m.lk.Lock()
	switch state.Kind {
	case AccountLimitAlertKindWarning:
		if last, ok := m.lastAlert[state.HostID]; !ok || state.SentAt.After(last) {
			m.lastAlert[state.HostID] = state.SentAt
		}
		m.aboveThreshold[state.HostID] = usage
	case AccountLimitAlertKindRecovery:
		if last, ok := m.lastAlert[state.HostID]; ok && last.After(state.SentAt) {
			m.lk.Unlock()
			return nil
		}
		delete(m.lastAlert, state.HostID)
		delete(m.aboveThreshold, state.HostID)
	}
	m.lk.Unlock()

	if notifySiblings && m.config.SentCallback != nil {
		m.config.SentCallback(ctx, state)
	}
	return nil
}

func (s *Service) handleAdminRecordAccountLimitAlertSent(c echo.Context) error {
	var body AccountLimitAlertSentState
	if err := c.Bind(&body); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid body: %s", err))
	}
	if s.accountLimitAlertRecorder == nil {
		return c.JSON(http.StatusOK, map[string]any{"success": "true"})
	}
	if err := s.accountLimitAlertRecorder.RecordAccountLimitAlertSent(body); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"success": "true"})
}

func formatAccountLimitRecoveryAlert(config AccountLimitAlertMonitorConfig, usage relay.HostAccountLimitUsage) string {
	env := strings.TrimSpace(config.Environment)
	header := "Relay PDS repo limit recovered"
	if env != "" {
		header = fmt.Sprintf("%s in %s", header, env)
	}
	return fmt.Sprintf(
		"%s\n%s is no longer above the %.1f%% repo-limit alert threshold (last alert state: %.1f%% used, %d/%d repos)",
		header,
		usage.Hostname,
		config.Threshold*100,
		usage.Usage*100,
		usage.AccountCount,
		usage.AccountLimit,
	)
}

func formatAccountLimitAlert(config AccountLimitAlertMonitorConfig, usages []relay.HostAccountLimitUsage) string {
	env := strings.TrimSpace(config.Environment)
	header := fmt.Sprintf("Relay PDS repo limit warning: %.1f%% threshold exceeded", config.Threshold*100)
	if env != "" {
		header = fmt.Sprintf("%s in %s", header, env)
	}

	lines := make([]string, 0, len(usages)+1)
	lines = append(lines, header)
	for _, usage := range usages {
		lines = append(lines, fmt.Sprintf(
			"%s: %.1f%% used (%d/%d repos)",
			usage.Hostname,
			usage.Usage*100,
			usage.AccountCount,
			usage.AccountLimit,
		))
	}
	return strings.Join(lines, "\n")
}

type SlackAlerter struct {
	client   *http.Client
	endpoint string
	token    string
	channel  string
}

func NewSlackAlerter(token, channel string) (*SlackAlerter, error) {
	token = strings.TrimSpace(token)
	channel = strings.TrimSpace(channel)
	if token == "" {
		return nil, fmt.Errorf("slack alert token is required")
	}
	if channel == "" {
		return nil, fmt.Errorf("slack alert channel is required")
	}
	client := cliutil.NewHttpClient()
	client.Timeout = 10 * time.Second
	return &SlackAlerter{
		client:   client,
		endpoint: "https://slack.com/api/chat.postMessage",
		token:    token,
		channel:  channel,
	}, nil
}

func (s *SlackAlerter) SendAlert(ctx context.Context, msg AlertMessage) error {
	payload := struct {
		Channel     string `json:"channel"`
		Text        string `json:"text"`
		Mrkdwn      bool   `json:"mrkdwn"`
		UnfurlLinks bool   `json:"unfurl_links"`
		UnfurlMedia bool   `json:"unfurl_media"`
	}{
		Channel:     s.channel,
		Text:        msg.Text,
		Mrkdwn:      false,
		UnfurlLinks: false,
		UnfurlMedia: false,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "application/json")

	client := s.client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("slack alert request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var slackResp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(respBody, &slackResp); err != nil {
		return fmt.Errorf("decoding slack alert response: %w", err)
	}
	if !slackResp.OK {
		if slackResp.Error == "" {
			slackResp.Error = "unknown error"
		}
		return fmt.Errorf("slack alert request failed: %s", slackResp.Error)
	}
	return nil
}
