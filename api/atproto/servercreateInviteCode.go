package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.server.createInviteCode

func init() {
}

type ServerCreateInviteCode_Input struct {
	UseCount int64 `json:"useCount" cborgen:"useCount"`
}

type ServerCreateInviteCode_Output struct {
	Code string `json:"code" cborgen:"code"`
}

func ServerCreateInviteCode(ctx context.Context, c *xrpc.Client, input *ServerCreateInviteCode_Input) (*ServerCreateInviteCode_Output, error) {
	var out ServerCreateInviteCode_Output
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.server.createInviteCode", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
