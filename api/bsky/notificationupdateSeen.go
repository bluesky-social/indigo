// Code generated by cmd/lexgen (see Makefile's lexgen); DO NOT EDIT.

package bsky

// schema: app.bsky.notification.updateSeen

import (
	"context"

	"github.com/bluesky-social/indigo/lex/util"
)

// NotificationUpdateSeen_Input is the input argument to a app.bsky.notification.updateSeen call.
type NotificationUpdateSeen_Input struct {
	SeenAt string `json:"seenAt" cborgen:"seenAt"`
}

// NotificationUpdateSeen calls the XRPC method "app.bsky.notification.updateSeen".
func NotificationUpdateSeen(ctx context.Context, c util.LexClient, input *NotificationUpdateSeen_Input) error {
	if err := c.LexDo(ctx, util.Procedure, "application/json", "app.bsky.notification.updateSeen", nil, input, nil); err != nil {
		return err
	}

	return nil
}
