package bsky

import (
	"context"

	"github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/xrpc"
)

// schema: app.bsky.notification.list

func init() {
}

type NotificationList_Notification struct {
	LexiconTypeID string                  `json:"$type,omitempty"`
	Author        *ActorRef_WithInfo      `json:"author" cborgen:"author"`
	Cid           string                  `json:"cid" cborgen:"cid"`
	IndexedAt     string                  `json:"indexedAt" cborgen:"indexedAt"`
	IsRead        bool                    `json:"isRead" cborgen:"isRead"`
	Reason        string                  `json:"reason" cborgen:"reason"`
	ReasonSubject *string                 `json:"reasonSubject,omitempty" cborgen:"reasonSubject"`
	Record        util.LexiconTypeDecoder `json:"record" cborgen:"record"`
	Uri           string                  `json:"uri" cborgen:"uri"`
}

type NotificationList_Output struct {
	LexiconTypeID string                           `json:"$type,omitempty"`
	Cursor        *string                          `json:"cursor,omitempty" cborgen:"cursor"`
	Notifications []*NotificationList_Notification `json:"notifications" cborgen:"notifications"`
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
