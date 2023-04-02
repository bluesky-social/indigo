package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.admin.getRepo

func init() {
}
func AdminGetRepo(ctx context.Context, c *xrpc.Client, did string) (*AdminDefs_RepoViewDetail, error) {
	var out AdminDefs_RepoViewDetail

	params := map[string]interface{}{
		"did": did,
	}
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.admin.getRepo", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
