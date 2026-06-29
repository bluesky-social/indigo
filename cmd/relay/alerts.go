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
	"net/url"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/cmd/relay/relay"
	"github.com/bluesky-social/indigo/cmd/relay/relay/models"
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
	Title    string
	Text     string
	Severity AlertSeverity
	Fields   []AlertField
	Actions  []AlertAction
}

type AlertSeverity string

const (
	AlertSeverityWarning  = AlertSeverity("warning")
	AlertSeverityRecovery = AlertSeverity("recovery")
)

type AlertField struct {
	Title string
	Value string
}

type AlertAction struct {
	Text string
	URL  string
}

type Alerter interface {
	SendAlert(ctx context.Context, msg AlertMessage) error
}

type accountLimitAlertClaimer interface {
	ClaimDueAccountLimitAlerts(ctx context.Context, threshold float64, repeatInterval time.Duration, limit int) (*relay.HostAccountLimitAlertClaims, error)
	RecordHostAccountLimitAlertSent(ctx context.Context, hostname string, state models.HostAccountLimitAlertState, sentAt time.Time) error
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
	DashboardURL    string
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
	if strings.TrimSpace(c.DashboardURL) != "" {
		u, err := url.Parse(c.DashboardURL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("account limit alert dashboard URL must be an absolute http or https URL")
		}
	}
	return nil
}

type AccountLimitAlertMonitor struct {
	logger  *slog.Logger
	claimer accountLimitAlertClaimer
	alerter Alerter
	config  AccountLimitAlertMonitorConfig

	now func() time.Time
}

func NewAccountLimitAlertMonitor(logger *slog.Logger, claimer accountLimitAlertClaimer, alerter Alerter, config AccountLimitAlertMonitorConfig) (*AccountLimitAlertMonitor, error) {
	if claimer == nil {
		return nil, fmt.Errorf("account limit alert claimer is required")
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
		logger:  logger.With("system", "account-limit-alerts"),
		claimer: claimer,
		alerter: alerter,
		config:  config,
		now:     time.Now,
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
	claims, err := m.claimer.ClaimDueAccountLimitAlerts(ctx, m.config.Threshold, m.config.RepeatInterval, 0)
	if err != nil {
		return err
	}

	for _, usage := range claims.Recoveries {
		if err := m.alerter.SendAlert(ctx, formatAccountLimitRecoveryAlert(m.config, usage)); err != nil {
			return err
		}
		m.forwardAccountLimitAlertSent(ctx, accountLimitAlertSentState(AccountLimitAlertKindRecovery, usage, usage.AlertSentAt))
	}

	for start := 0; start < len(claims.Warnings); start += m.config.HostsPerMessage {
		end := start + m.config.HostsPerMessage
		if end > len(claims.Warnings) {
			end = len(claims.Warnings)
		}
		batch := claims.Warnings[start:end]
		if err := m.alerter.SendAlert(ctx, formatAccountLimitAlert(m.config, batch)); err != nil {
			return err
		}
		for _, usage := range batch {
			m.forwardAccountLimitAlertSent(ctx, accountLimitAlertSentState(AccountLimitAlertKindWarning, usage, usage.AlertSentAt))
		}
	}
	return nil
}

func (m *AccountLimitAlertMonitor) forwardAccountLimitAlertSent(ctx context.Context, state AccountLimitAlertSentState) {
	if m.config.SentCallback != nil {
		m.config.SentCallback(ctx, state)
	}
}

func (s *Service) handleAdminRecordAccountLimitAlertSent(c echo.Context) error {
	var body AccountLimitAlertSentState
	if err := c.Bind(&body); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid body: %s", err))
	}
	if err := body.normalize(time.Now().UTC()); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	state := models.HostAccountLimitAlertStateWarning
	if body.Kind == AccountLimitAlertKindRecovery {
		state = models.HostAccountLimitAlertStateOK
	}
	if err := s.relay.RecordHostAccountLimitAlertSent(c.Request().Context(), body.Hostname, state, body.SentAt); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]any{"success": "true"})
}

func formatAccountLimitRecoveryAlert(config AccountLimitAlertMonitorConfig, usage relay.HostAccountLimitUsage) AlertMessage {
	env := strings.TrimSpace(config.Environment)
	title := "PDS repo limit recovered"
	if env != "" {
		title = fmt.Sprintf("%s in %s", title, env)
	}
	msg := AlertMessage{
		Title:    title,
		Severity: AlertSeverityRecovery,
		Text: fmt.Sprintf(
			"`%s` is no longer above the %.1f%% repo-limit alert threshold.",
			usage.Hostname,
			config.Threshold*100,
		),
		Fields: []AlertField{
			{Title: "Last alert state", Value: fmt.Sprintf("%.1f%% used", usage.Usage*100)},
			{Title: "Last repo count", Value: fmt.Sprintf("%d / %d", usage.AccountCount, usage.AccountLimit)},
			{Title: "Threshold", Value: fmt.Sprintf("%.1f%%", config.Threshold*100)},
		},
	}
	if env != "" {
		msg.Fields = append(msg.Fields, AlertField{Title: "Environment", Value: env})
	}
	if strings.TrimSpace(config.DashboardURL) != "" {
		msg.Actions = append(msg.Actions, AlertAction{Text: "Open relay dashboard", URL: strings.TrimSpace(config.DashboardURL)})
	}
	return msg
}

func formatAccountLimitAlert(config AccountLimitAlertMonitorConfig, usages []relay.HostAccountLimitUsage) AlertMessage {
	env := strings.TrimSpace(config.Environment)
	title := "PDS repo limit warning"
	if env != "" {
		title = fmt.Sprintf("%s in %s", title, env)
	}

	lines := make([]string, 0, len(usages)+1)
	lines = append(lines, fmt.Sprintf("%d PDS host(s) are above the %.1f%% repo-limit alert threshold.", len(usages), config.Threshold*100))
	highestUsage := float64(0)
	for _, usage := range usages {
		if usage.Usage > highestUsage {
			highestUsage = usage.Usage
		}
		lines = append(lines, fmt.Sprintf(
			"- `%s`: *%.1f%%* used (`%d / %d` repos)",
			usage.Hostname,
			usage.Usage*100,
			usage.AccountCount,
			usage.AccountLimit,
		))
	}
	msg := AlertMessage{
		Title:    title,
		Severity: AlertSeverityWarning,
		Text:     strings.Join(lines, "\n"),
		Fields: []AlertField{
			{Title: "Threshold", Value: fmt.Sprintf("%.1f%%", config.Threshold*100)},
			{Title: "Hosts", Value: fmt.Sprintf("%d", len(usages))},
			{Title: "Highest usage", Value: fmt.Sprintf("%.1f%%", highestUsage*100)},
		},
	}
	if env != "" {
		msg.Fields = append(msg.Fields, AlertField{Title: "Environment", Value: env})
	}
	if strings.TrimSpace(config.DashboardURL) != "" {
		msg.Actions = append(msg.Actions, AlertAction{Text: "Open relay dashboard", URL: strings.TrimSpace(config.DashboardURL)})
	}
	return msg
}

type SlackAlerter struct {
	client   *http.Client
	endpoint string
	token    string
	channel  string
}

type slackText struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Emoji bool   `json:"emoji,omitempty"`
}

type slackBlock struct {
	Type     string      `json:"type"`
	Text     *slackText  `json:"text,omitempty"`
	Fields   []slackText `json:"fields,omitempty"`
	Elements []any       `json:"elements,omitempty"`
}

type slackElement struct {
	Type     string    `json:"type"`
	Text     slackText `json:"text"`
	URL      string    `json:"url,omitempty"`
	Style    string    `json:"style,omitempty"`
	ActionID string    `json:"action_id,omitempty"`
}

type slackContextElement struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type slackAttachment struct {
	Color  string       `json:"color"`
	Blocks []slackBlock `json:"blocks"`
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
		Channel     string            `json:"channel"`
		Text        string            `json:"text"`
		Attachments []slackAttachment `json:"attachments,omitempty"`
		UnfurlLinks bool              `json:"unfurl_links"`
		UnfurlMedia bool              `json:"unfurl_media"`
	}{
		Channel:     s.channel,
		Text:        slackFallbackText(msg),
		Attachments: []slackAttachment{slackAttachmentForAlert(msg)},
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

func slackFallbackText(msg AlertMessage) string {
	if strings.TrimSpace(msg.Title) == "" {
		return msg.Text
	}
	if strings.TrimSpace(msg.Text) == "" {
		return msg.Title
	}
	return msg.Title + "\n" + msg.Text
}

func slackAttachmentForAlert(msg AlertMessage) slackAttachment {
	blocks := []slackBlock{}
	if strings.TrimSpace(msg.Title) != "" {
		blocks = append(blocks, slackBlock{
			Type: "header",
			Text: &slackText{Type: "plain_text", Text: msg.Title, Emoji: true},
		})
	}
	if strings.TrimSpace(msg.Text) != "" {
		blocks = append(blocks, slackBlock{
			Type: "section",
			Text: &slackText{Type: "mrkdwn", Text: msg.Text},
		})
	}
	if len(msg.Fields) > 0 {
		fields := make([]slackText, 0, len(msg.Fields))
		for i, field := range msg.Fields {
			if i >= 10 {
				break
			}
			fields = append(fields, slackText{
				Type: "mrkdwn",
				Text: fmt.Sprintf("*%s*\n%s", field.Title, field.Value),
			})
		}
		blocks = append(blocks, slackBlock{Type: "section", Fields: fields})
	}
	if len(msg.Actions) > 0 {
		elements := make([]any, 0, len(msg.Actions))
		for i, action := range msg.Actions {
			if i >= 5 {
				break
			}
			if strings.TrimSpace(action.Text) == "" || strings.TrimSpace(action.URL) == "" {
				continue
			}
			elements = append(elements, slackElement{
				Type:     "button",
				Text:     slackText{Type: "plain_text", Text: action.Text, Emoji: true},
				URL:      action.URL,
				Style:    "primary",
				ActionID: fmt.Sprintf("relay_alert_action_%d", i),
			})
		}
		if len(elements) > 0 {
			blocks = append(blocks, slackBlock{Type: "actions", Elements: elements})
		}
	}
	blocks = append(blocks, slackBlock{
		Type: "context",
		Elements: []any{
			slackContextElement{Type: "mrkdwn", Text: "indigo relay account-limit monitor"},
		},
	})
	return slackAttachment{
		Color:  slackColorForSeverity(msg.Severity),
		Blocks: blocks,
	}
}

func slackColorForSeverity(severity AlertSeverity) string {
	switch severity {
	case AlertSeverityRecovery:
		return "#2EB67D"
	case AlertSeverityWarning:
		return "#ECB22E"
	default:
		return "#439FE0"
	}
}
