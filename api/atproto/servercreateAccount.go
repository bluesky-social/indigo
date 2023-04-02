package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.server.createAccount

func init() {
}

type ServerCreateAccount_Input struct {
	LexiconTypeID string  `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Email         string  `json:"email" cborgen:"email"`
	Handle        string  `json:"handle" cborgen:"handle"`
	InviteCode    *string `json:"inviteCode,omitempty" cborgen:"inviteCode,omitempty"`
	Password      string  `json:"password" cborgen:"password"`
	RecoveryKey   *string `json:"recoveryKey,omitempty" cborgen:"recoveryKey,omitempty"`
}

type ServerCreateAccount_Output struct {
	LexiconTypeID string `json:"$type,omitempty" cborgen:"$type,omitempty"`
	AccessJwt     string `json:"accessJwt" cborgen:"accessJwt"`
	Did           string `json:"did" cborgen:"did"`
	Handle        string `json:"handle" cborgen:"handle"`
	RefreshJwt    string `json:"refreshJwt" cborgen:"refreshJwt"`
}

func ServerCreateAccount(ctx context.Context, c *xrpc.Client, input *ServerCreateAccount_Input) (*ServerCreateAccount_Output, error) {
	var out ServerCreateAccount_Output
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.server.createAccount", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
