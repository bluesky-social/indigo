package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.account.resetPassword

type AccountResetPassword_Input struct {
	Token    string `json:"token" cborgen:"token"`
	Password string `json:"password" cborgen:"password"`
}

func (t *AccountResetPassword_Input) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["password"] = t.Password
	out["token"] = t.Token
	return json.Marshal(out)
}

func AccountResetPassword(ctx context.Context, c *xrpc.Client, input AccountResetPassword_Input) error {
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.account.resetPassword", nil, input, nil); err != nil {
		return err
	}

	return nil
}
