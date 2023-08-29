// Code generated by cmd/lexgen (see Makefile's lexgen); DO NOT EDIT.

package atproto

// schema: com.atproto.server.createAccount

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// ServerCreateAccount_Input is the input argument to a com.atproto.server.createAccount call.
type ServerCreateAccount_Input struct {
	Did         *string `json:"did,omitempty" cborgen:"did,omitempty"`
	Email       string  `json:"email" cborgen:"email"`
	Handle      string  `json:"handle" cborgen:"handle"`
	InviteCode  *string `json:"inviteCode,omitempty" cborgen:"inviteCode,omitempty"`
	Password    string  `json:"password" cborgen:"password"`
	RecoveryKey *string `json:"recoveryKey,omitempty" cborgen:"recoveryKey,omitempty"`
}

// ServerCreateAccount_Output is the output of a com.atproto.server.createAccount call.
type ServerCreateAccount_Output struct {
	AccessJwt  string `json:"accessJwt" cborgen:"accessJwt"`
	Did        string `json:"did" cborgen:"did"`
	Handle     string `json:"handle" cborgen:"handle"`
	RefreshJwt string `json:"refreshJwt" cborgen:"refreshJwt"`
}

// ServerCreateAccount calls the XRPC method "com.atproto.server.createAccount".
func ServerCreateAccount(ctx context.Context, c *xrpc.Client, input *ServerCreateAccount_Input) (*ServerCreateAccount_Output, error) {
	var out ServerCreateAccount_Output
	c.Mux.RLock()
	defer c.Mux.RUnlock()
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.server.createAccount", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
