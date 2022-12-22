package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.notification.list

func init() {
}

type NotificationList_Output struct {
	Cursor        *string                          `json:"cursor" cborgen:"cursor"`
	Notifications []*NotificationList_Notification `json:"notifications" cborgen:"notifications"`
}

type NotificationList_Notification struct {
	Author        *ActorRef_WithInfo `json:"author" cborgen:"author"`
	Cid           string             `json:"cid" cborgen:"cid"`
	IndexedAt     string             `json:"indexedAt" cborgen:"indexedAt"`
	IsRead        bool               `json:"isRead" cborgen:"isRead"`
	Reason        string             `json:"reason" cborgen:"reason"`
	ReasonSubject *string            `json:"reasonSubject" cborgen:"reasonSubject"`
	Record        any                `json:"record" cborgen:"record"`
	Uri           string             `json:"uri" cborgen:"uri"`
}

func NotificationList(ctx context.Context, c *xrpc.Client, before string, limit int64) (*NotificationList_Output, error) {
	var out NotificationList_Output

	params := map[string]interface{}{
		"before": before,
		"limit":  limit,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.notification.list", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
