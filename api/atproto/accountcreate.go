package schemagen

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.account.create

func init() {
}

type AccountCreate_Input struct {
	LexiconTypeID string  `json:"$type,omitempty"`
	Email         string  `json:"email" cborgen:"email"`
	Handle        string  `json:"handle" cborgen:"handle"`
	InviteCode    *string `json:"inviteCode,omitempty" cborgen:"inviteCode"`
	Password      string  `json:"password" cborgen:"password"`
	RecoveryKey   *string `json:"recoveryKey,omitempty" cborgen:"recoveryKey"`
}

type AccountCreate_Output struct {
	LexiconTypeID string `json:"$type,omitempty"`
	AccessJwt     string `json:"accessJwt" cborgen:"accessJwt"`
	Did           string `json:"did" cborgen:"did"`
	Handle        string `json:"handle" cborgen:"handle"`
	RefreshJwt    string `json:"refreshJwt" cborgen:"refreshJwt"`
}

func AccountCreate(ctx context.Context, c *xrpc.Client, input *AccountCreate_Input) (*AccountCreate_Output, error) {
	var out AccountCreate_Output
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.account.create", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
