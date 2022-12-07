package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.account.createInviteCode

type AccountCreateInviteCode_Input struct {
	UseCount int64 `json:"useCount"`
}

func (t *AccountCreateInviteCode_Input) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["useCount"] = t.UseCount
	return json.Marshal(out)
}

type AccountCreateInviteCode_Output struct {
	Code string `json:"code"`
}

func (t *AccountCreateInviteCode_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["code"] = t.Code
	return json.Marshal(out)
}

func AccountCreateInviteCode(ctx context.Context, c *xrpc.Client, input AccountCreateInviteCode_Input) (*AccountCreateInviteCode_Output, error) {
	var out AccountCreateInviteCode_Output
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.account.createInviteCode", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
