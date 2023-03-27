package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.server.getSession

func init() {
}

type ServerGetSession_Output struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Did           string `json:"did" cborgen:"did"`
	Handle        string `json:"handle" cborgen:"handle"`
}

func ServerGetSession(ctx context.Context, c *xrpc.Client) (*ServerGetSession_Output, error) {
	var out ServerGetSession_Output
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.server.getSession", nil, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
