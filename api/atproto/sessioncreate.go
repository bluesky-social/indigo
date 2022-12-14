package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.session.create

func init() {
}

type SessionCreate_Input struct {
	Handle   string `json:"handle" cborgen:"handle"`
	Password string `json:"password" cborgen:"password"`
}

type SessionCreate_Output struct {
	Did        string `json:"did" cborgen:"did"`
	AccessJwt  string `json:"accessJwt" cborgen:"accessJwt"`
	RefreshJwt string `json:"refreshJwt" cborgen:"refreshJwt"`
	Handle     string `json:"handle" cborgen:"handle"`
}

func SessionCreate(ctx context.Context, c *xrpc.Client, input SessionCreate_Input) (*SessionCreate_Output, error) {
	var out SessionCreate_Output
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.session.create", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
