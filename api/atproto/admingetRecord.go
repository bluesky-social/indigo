package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.admin.getRecord

func init() {
}
func AdminGetRecord(ctx context.Context, c *xrpc.Client, cid string, uri string) (*AdminDefs_RecordViewDetail, error) {
	var out AdminDefs_RecordViewDetail

	params := map[string]interface{}{
		"cid": cid,
		"uri": uri,
	}
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.admin.getRecord", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
