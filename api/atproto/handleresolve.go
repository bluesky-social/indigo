package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.handle.resolve

func init() {
}

type HandleResolve_Output struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Did           string `json:"did" cborgen:"did"`
}

func HandleResolve(ctx context.Context, c *xrpc.Client, handle string) (*HandleResolve_Output, error) {
	var out HandleResolve_Output

	params := map[string]interface{}{
		"handle": handle,
	}
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.handle.resolve", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
