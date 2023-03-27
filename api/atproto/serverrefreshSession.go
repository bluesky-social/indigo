package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.server.refreshSession

func init() {
}

type ServerRefreshSession_Output struct {
	LexiconTypeID string `json:"$type,omitempty"`
	AccessJwt     string `json:"accessJwt" cborgen:"accessJwt"`
	Did           string `json:"did" cborgen:"did"`
	Handle        string `json:"handle" cborgen:"handle"`
	RefreshJwt    string `json:"refreshJwt" cborgen:"refreshJwt"`
}

func ServerRefreshSession(ctx context.Context, c *xrpc.Client) (*ServerRefreshSession_Output, error) {
	var out ServerRefreshSession_Output
	if err := c.Do(ctx, xrpc.Procedure, "", "com.atproto.server.refreshSession", nil, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
