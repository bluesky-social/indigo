package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.account.createInviteCode

func init() {
}

type AccountCreateInviteCode_Input struct {
	UseCount int64 `json:"useCount" cborgen:"useCount"`
}

type AccountCreateInviteCode_Output struct {
	Code string `json:"code" cborgen:"code"`
}

func AccountCreateInviteCode(ctx context.Context, c *xrpc.Client, input AccountCreateInviteCode_Input) (*AccountCreateInviteCode_Output, error) {
	var out AccountCreateInviteCode_Output
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.account.createInviteCode", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
