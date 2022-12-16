package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.session.refresh

func init() {
}

type SessionRefresh_Output struct {
	Did        string `json:"did" cborgen:"did"`
	AccessJwt  string `json:"accessJwt" cborgen:"accessJwt"`
	RefreshJwt string `json:"refreshJwt" cborgen:"refreshJwt"`
	Handle     string `json:"handle" cborgen:"handle"`
}

func SessionRefresh(ctx context.Context, c *xrpc.Client) (*SessionRefresh_Output, error) {
	var out SessionRefresh_Output
	if err := c.Do(ctx, xrpc.Procedure, "", "com.atproto.session.refresh", nil, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
