package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.admin.getModerationReport

func init() {
}
func AdminGetModerationReport(ctx context.Context, c *xrpc.Client, id int64) (*AdminDefs_ReportViewDetail, error) {
	var out AdminDefs_ReportViewDetail

	params := map[string]interface{}{
		"id": id,
	}
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.admin.getModerationReport", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
