package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.admin.updateAccountHandle

func init() {
}

type AdminUpdateAccountHandle_Input struct {
	Did    string `json:"did" cborgen:"did"`
	Handle string `json:"handle" cborgen:"handle"`
}

func AdminUpdateAccountHandle(ctx context.Context, c *xrpc.Client, input *AdminUpdateAccountHandle_Input) error {
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.admin.updateAccountHandle", nil, input, nil); err != nil {
		return err
	}

	return nil
}
