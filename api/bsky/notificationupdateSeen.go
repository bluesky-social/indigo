package bsky

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: app.bsky.notification.updateSeen

func init() {
}

type NotificationUpdateSeen_Input struct {
	SeenAt string `json:"seenAt" cborgen:"seenAt"`
}

func NotificationUpdateSeen(ctx context.Context, c *xrpc.Client, input *NotificationUpdateSeen_Input) error {
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "app.bsky.notification.updateSeen", nil, input, nil); err != nil {
		return err
	}

	return nil
}
