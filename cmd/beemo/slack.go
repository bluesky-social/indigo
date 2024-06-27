package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/util"
)

type SlackWebhookBody struct {
	Text string `json:"text"`
}

// sends a simple slack message to a channel via "incoming webhook"
// The slack incoming webhook must be already configured in the slack workplace.
func sendSlackMsg(ctx context.Context, msg, webhookURL string) error {
	// loosely based on: https://golangcode.com/send-slack-messages-without-a-library/

	body, _ := json.Marshal(SlackWebhookBody{Text: msg})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	req.Header.Add("Content-Type", "application/json")
	client := util.RobustHTTPClient()
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
