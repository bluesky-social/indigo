package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.account.resetPassword

func init() {
}

type AccountResetPassword_Input struct {
	Password string `json:"password" cborgen:"password"`
	Token    string `json:"token" cborgen:"token"`
}

func AccountResetPassword(ctx context.Context, c *xrpc.Client, input AccountResetPassword_Input) error {
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.account.resetPassword", nil, input, nil); err != nil {
		return err
	}

	return nil
}
