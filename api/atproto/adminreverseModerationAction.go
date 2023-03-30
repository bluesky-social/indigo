package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.admin.reverseModerationAction

func init() {
}

type AdminReverseModerationAction_Input struct {
	LexiconTypeID string `json:"$type,omitempty" cborgen:"$type,omitempty"`
	CreatedBy     string `json:"createdBy" cborgen:"createdBy"`
	Id            int64  `json:"id" cborgen:"id"`
	Reason        string `json:"reason" cborgen:"reason"`
}

func AdminReverseModerationAction(ctx context.Context, c *xrpc.Client, input *AdminReverseModerationAction_Input) (*AdminDefs_ActionView, error) {
	var out AdminDefs_ActionView
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.admin.reverseModerationAction", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
