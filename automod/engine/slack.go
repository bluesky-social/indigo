package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type SlackNotifier struct {
	SlackWebhookURL string
}

func (n *SlackNotifier) SendAccount(ctx context.Context, service string, c *AccountContext) error {
	if service != "slack" {
		return nil
	}
	msg := slackBody("⚠️ Automod Account Action ⚠️\n", c.Account, c.effects.AccountLabels, c.effects.AccountFlags, c.effects.AccountReports, c.effects.AccountTakedown)
	c.Logger.Debug("sending slack notification")
	return n.sendSlackMsg(ctx, msg)
}

func (n *SlackNotifier) SendRecord(ctx context.Context, service string, c *RecordContext) error {
	if service != "slack" {
		return nil
	}
	atURI := fmt.Sprintf("at://%s/%s/%s", c.Account.Identity.DID, c.RecordOp.Collection, c.RecordOp.RecordKey)
	msg := slackBody("⚠️ Automod Record Action ⚠️\n", c.Account, c.effects.RecordLabels, c.effects.RecordFlags, c.effects.RecordReports, c.effects.RecordTakedown)
	msg += fmt.Sprintf("`%s`\n", atURI)
	c.Logger.Debug("sending slack notification")
	return n.sendSlackMsg(ctx, msg)
}

type SlackWebhookBody struct {
	Text string `json:"text"`
}

// Sends a simple slack message to a channel via "incoming webhook".
//
// The slack incoming webhook must be already configured in the slack workplace.
func (n *SlackNotifier) sendSlackMsg(ctx context.Context, msg string) error {
	// loosely based on: https://golangcode.com/send-slack-messages-without-a-library/

	body, err := json.Marshal(SlackWebhookBody{Text: msg})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.SlackWebhookURL, bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	req.Header.Add("Content-Type", "application/json")
	client := http.DefaultClient
	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	if resp.StatusCode != 200 || buf.String() != "ok" {
		return fmt.Errorf("failed slack webhook POST request. status=%d", resp.StatusCode)
	}
	return nil
}

func slackBody(header string, acct AccountMeta, newLabels, newFlags []string, newReports []ModReport, newTakedown bool) string {
	msg := header
	msg += fmt.Sprintf("`%s` / `%s` / <https://bsky.app/profile/%s|bsky> / <https://admin.prod.bsky.dev/repositories/%s|ozone>\n",
		acct.Identity.DID,
		acct.Identity.Handle,
		acct.Identity.DID,
		acct.Identity.DID,
	)
	if len(newLabels) > 0 {
		msg += fmt.Sprintf("Labels: `%s`\n", strings.Join(newLabels, ", "))
	}
	if len(newFlags) > 0 {
		msg += fmt.Sprintf("Flags: `%s`\n", strings.Join(newFlags, ", "))
	}
	for _, rep := range newReports {
		msg += fmt.Sprintf("Report `%s`: %s\n", rep.ReasonType, rep.Comment)
	}
	if newTakedown {
		msg += "Takedown!\n"
	}
	return msg
}
