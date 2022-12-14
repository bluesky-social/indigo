package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.account.requestPasswordReset

func init() {
}

type AccountRequestPasswordReset_Input struct {
	Email string `json:"email" cborgen:"email"`
}

func AccountRequestPasswordReset(ctx context.Context, c *xrpc.Client, input AccountRequestPasswordReset_Input) error {
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.account.requestPasswordReset", nil, input, nil); err != nil {
		return err
	}

	return nil
}
