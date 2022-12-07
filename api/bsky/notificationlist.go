package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.notification.list

type NotificationList_Output struct {
	Cursor        string                           `json:"cursor"`
	Notifications []*NotificationList_Notification `json:"notifications"`
}

func (t *NotificationList_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["cursor"] = t.Cursor
	out["notifications"] = t.Notifications
	return json.Marshal(out)
}

type NotificationList_Notification struct {
	Author        *ActorRef_WithInfo `json:"author"`
	Reason        string             `json:"reason"`
	ReasonSubject string             `json:"reasonSubject"`
	Record        any                `json:"record"`
	IsRead        bool               `json:"isRead"`
	IndexedAt     string             `json:"indexedAt"`
	Uri           string             `json:"uri"`
	Cid           string             `json:"cid"`
}

func (t *NotificationList_Notification) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["author"] = t.Author
	out["cid"] = t.Cid
	out["indexedAt"] = t.IndexedAt
	out["isRead"] = t.IsRead
	out["reason"] = t.Reason
	out["reasonSubject"] = t.ReasonSubject
	out["record"] = t.Record
	out["uri"] = t.Uri
	return json.Marshal(out)
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
