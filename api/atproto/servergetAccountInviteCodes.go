package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.server.getAccountInviteCodes

func init() {
}

type ServerGetAccountInviteCodes_Output struct {
	Codes []*ServerDefs_InviteCode `json:"codes" cborgen:"codes"`
}

func ServerGetAccountInviteCodes(ctx context.Context, c *xrpc.Client, createAvailable bool, includeUsed bool) (*ServerGetAccountInviteCodes_Output, error) {
	var out ServerGetAccountInviteCodes_Output

	params := map[string]interface{}{
		"createAvailable": createAvailable,
		"includeUsed":     includeUsed,
	}
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.server.getAccountInviteCodes", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
