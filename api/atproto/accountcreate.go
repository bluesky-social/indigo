package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.account.create

type AccountCreate_Input struct {
	Email       string `json:"email"`
	Handle      string `json:"handle"`
	InviteCode  string `json:"inviteCode"`
	Password    string `json:"password"`
	RecoveryKey string `json:"recoveryKey"`
}

func (t *AccountCreate_Input) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["email"] = t.Email
	out["handle"] = t.Handle
	out["inviteCode"] = t.InviteCode
	out["password"] = t.Password
	out["recoveryKey"] = t.RecoveryKey
	return json.Marshal(out)
}

type AccountCreate_Output struct {
	Handle     string `json:"handle"`
	Did        string `json:"did"`
	AccessJwt  string `json:"accessJwt"`
	RefreshJwt string `json:"refreshJwt"`
}

func (t *AccountCreate_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["accessJwt"] = t.AccessJwt
	out["did"] = t.Did
	out["handle"] = t.Handle
	out["refreshJwt"] = t.RefreshJwt
	return json.Marshal(out)
}

func AccountCreate(ctx context.Context, c *xrpc.Client, input AccountCreate_Input) (*AccountCreate_Output, error) {
	var out AccountCreate_Output
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.account.create", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
