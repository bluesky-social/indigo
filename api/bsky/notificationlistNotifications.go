package bsky

import (
	"context"

	"github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/xrpc"
)

// schema: app.bsky.notification.listNotifications

func init() {
}

type NotificationListNotifications_Notification struct {
	LexiconTypeID string                  `json:"$type,omitempty"`
	Author        *ActorDefs_WithInfo     `json:"author" cborgen:"author"`
	Cid           string                  `json:"cid" cborgen:"cid"`
	IndexedAt     string                  `json:"indexedAt" cborgen:"indexedAt"`
	IsRead        bool                    `json:"isRead" cborgen:"isRead"`
	Reason        string                  `json:"reason" cborgen:"reason"`
	ReasonSubject *string                 `json:"reasonSubject,omitempty" cborgen:"reasonSubject"`
	Record        util.LexiconTypeDecoder `json:"record" cborgen:"record"`
	Uri           string                  `json:"uri" cborgen:"uri"`
}

type NotificationListNotifications_Output struct {
	LexiconTypeID string                                        `json:"$type,omitempty"`
	Cursor        *string                                       `json:"cursor,omitempty" cborgen:"cursor"`
	Notifications []*NotificationListNotifications_Notification `json:"notifications" cborgen:"notifications"`
}

func NotificationListNotifications(ctx context.Context, c *xrpc.Client, cursor string, limit int64) (*NotificationListNotifications_Output, error) {
	var out NotificationListNotifications_Output

	params := map[string]interface{}{
		"cursor": cursor,
		"limit":  limit,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.notification.listNotifications", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
