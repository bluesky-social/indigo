package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.handle.update

func init() {
}

type HandleUpdate_Input struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Handle        string `json:"handle" cborgen:"handle"`
}

func HandleUpdate(ctx context.Context, c *xrpc.Client, input *HandleUpdate_Input) error {
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.handle.update", nil, input, nil); err != nil {
		return err
	}

	return nil
}
