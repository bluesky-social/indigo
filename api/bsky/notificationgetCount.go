package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.notification.getCount

func init() {
}

type NotificationGetCount_Output struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Count         int64  `json:"count" cborgen:"count"`
}

func NotificationGetCount(ctx context.Context, c *xrpc.Client) (*NotificationGetCount_Output, error) {
	var out NotificationGetCount_Output
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.notification.getCount", nil, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
