// Code generated by cmd/lexgen (see Makefile's lexgen); DO NOT EDIT.

package atproto

// schema: com.atproto.server.getSession

import (
	"context"

	"github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/xrpc"
)

// ServerGetSession_Output is the output of a com.atproto.server.getSession call.
type ServerGetSession_Output struct {
	Did    util.FormatDID    `json:"did" cborgen:"did"`
	Email  *string           `json:"email,omitempty" cborgen:"email,omitempty"`
	Handle util.FormatHandle `json:"handle" cborgen:"handle"`
}

// ServerGetSession calls the XRPC method "com.atproto.server.getSession".
func ServerGetSession(ctx context.Context, c *xrpc.Client) (*ServerGetSession_Output, error) {
	var out ServerGetSession_Output
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.server.getSession", nil, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
