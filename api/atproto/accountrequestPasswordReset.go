package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.account.requestPasswordReset

type AccountRequestPasswordReset_Input struct {
	Email string `json:"email" cborgen:"email"`
}

func (t *AccountRequestPasswordReset_Input) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["email"] = t.Email
	return json.Marshal(out)
}

func AccountRequestPasswordReset(ctx context.Context, c *xrpc.Client, input AccountRequestPasswordReset_Input) error {
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.account.requestPasswordReset", nil, input, nil); err != nil {
		return err
	}

	return nil
}
