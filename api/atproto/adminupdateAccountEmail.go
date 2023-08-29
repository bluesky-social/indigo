// Code generated by cmd/lexgen (see Makefile's lexgen); DO NOT EDIT.

package atproto

// schema: com.atproto.admin.updateAccountEmail

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// AdminUpdateAccountEmail_Input is the input argument to a com.atproto.admin.updateAccountEmail call.
type AdminUpdateAccountEmail_Input struct {
	// account: The handle or DID of the repo.
	Account string `json:"account" cborgen:"account"`
	Email   string `json:"email" cborgen:"email"`
}

// AdminUpdateAccountEmail calls the XRPC method "com.atproto.admin.updateAccountEmail".
func AdminUpdateAccountEmail(ctx context.Context, c *xrpc.Client, input *AdminUpdateAccountEmail_Input) error {
	c.Mux.RLock()
	defer c.Mux.RUnlock()
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.admin.updateAccountEmail", nil, input, nil); err != nil {
		return err
	}

	return nil
}
