package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.notification.getCount

type NotificationGetCount_Output struct {
	Count int64 `json:"count" cborgen:"count"`
}

func (t *NotificationGetCount_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["count"] = t.Count
	return json.Marshal(out)
}

func NotificationGetCount(ctx context.Context, c *xrpc.Client) (*NotificationGetCount_Output, error) {
	var out NotificationGetCount_Output
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.notification.getCount", nil, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
