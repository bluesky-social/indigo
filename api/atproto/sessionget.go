package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.session.get

func init() {
}

type SessionGet_Output struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Did           string `json:"did" cborgen:"did"`
	Handle        string `json:"handle" cborgen:"handle"`
}

func SessionGet(ctx context.Context, c *xrpc.Client) (*SessionGet_Output, error) {
	var out SessionGet_Output
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.session.get", nil, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
