package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.account.create

type AccountCreate_Input struct {
	RecoveryKey string `json:"recoveryKey" cborgen:"recoveryKey"`
	Email       string `json:"email" cborgen:"email"`
	Handle      string `json:"handle" cborgen:"handle"`
	InviteCode  string `json:"inviteCode" cborgen:"inviteCode"`
	Password    string `json:"password" cborgen:"password"`
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
	AccessJwt  string `json:"accessJwt" cborgen:"accessJwt"`
	RefreshJwt string `json:"refreshJwt" cborgen:"refreshJwt"`
	Handle     string `json:"handle" cborgen:"handle"`
	Did        string `json:"did" cborgen:"did"`
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
