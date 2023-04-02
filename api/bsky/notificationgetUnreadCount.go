package bsky

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: app.bsky.notification.getUnreadCount

func init() {
}

type NotificationGetUnreadCount_Output struct {
	LexiconTypeID string `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Count         int64  `json:"count" cborgen:"count"`
}

func NotificationGetUnreadCount(ctx context.Context, c *xrpc.Client) (*NotificationGetUnreadCount_Output, error) {
	var out NotificationGetUnreadCount_Output
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.notification.getUnreadCount", nil, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
