package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.server.createAppPassword

func init() {
}

type ServerCreateAppPassword_AppPassword struct {
	CreatedAt string `json:"createdAt" cborgen:"createdAt"`
	Name      string `json:"name" cborgen:"name"`
	Password  string `json:"password" cborgen:"password"`
}

type ServerCreateAppPassword_Input struct {
	Name string `json:"name" cborgen:"name"`
}

func ServerCreateAppPassword(ctx context.Context, c *xrpc.Client, input *ServerCreateAppPassword_Input) (*ServerCreateAppPassword_AppPassword, error) {
	var out ServerCreateAppPassword_AppPassword
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.server.createAppPassword", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
