package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.account.create

func init() {
}

type AccountCreate_Input struct {
	Email       string  `json:"email" cborgen:"email"`
	Handle      string  `json:"handle" cborgen:"handle"`
	InviteCode  *string `json:"inviteCode" cborgen:"inviteCode"`
	Password    string  `json:"password" cborgen:"password"`
	RecoveryKey *string `json:"recoveryKey" cborgen:"recoveryKey"`
}

type AccountCreate_Output struct {
	Handle     string `json:"handle" cborgen:"handle"`
	Did        string `json:"did" cborgen:"did"`
	AccessJwt  string `json:"accessJwt" cborgen:"accessJwt"`
	RefreshJwt string `json:"refreshJwt" cborgen:"refreshJwt"`
}

func AccountCreate(ctx context.Context, c *xrpc.Client, input AccountCreate_Input) (*AccountCreate_Output, error) {
	var out AccountCreate_Output
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.account.create", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
