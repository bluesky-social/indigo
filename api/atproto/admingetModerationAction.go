package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.admin.getModerationAction

func init() {
}
func AdminGetModerationAction(ctx context.Context, c *xrpc.Client, id int64) (*AdminDefs_ActionViewDetail, error) {
	var out AdminDefs_ActionViewDetail

	params := map[string]interface{}{
		"id": id,
	}
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.admin.getModerationAction", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
