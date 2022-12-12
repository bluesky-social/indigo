package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.notification.updateSeen

type NotificationUpdateSeen_Input struct {
	SeenAt string `json:"seenAt" cborgen:"seenAt"`
}

func (t *NotificationUpdateSeen_Input) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["seenAt"] = t.SeenAt
	return json.Marshal(out)
}

func NotificationUpdateSeen(ctx context.Context, c *xrpc.Client, input NotificationUpdateSeen_Input) error {
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "app.bsky.notification.updateSeen", nil, input, nil); err != nil {
		return err
	}

	return nil
}
