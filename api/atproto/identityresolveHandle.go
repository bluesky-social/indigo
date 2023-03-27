package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.identity.resolveHandle

func init() {
}

type IdentityResolveHandle_Output struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Did           string `json:"did" cborgen:"did"`
}

func IdentityResolveHandle(ctx context.Context, c *xrpc.Client, handle string) (*IdentityResolveHandle_Output, error) {
	var out IdentityResolveHandle_Output

	params := map[string]interface{}{
		"handle": handle,
	}
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.identity.resolveHandle", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
