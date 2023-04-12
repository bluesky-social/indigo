package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.server.createSession

func init() {
}

type ServerCreateSession_Input struct {
	Identifier string `json:"identifier" cborgen:"identifier"`
	Password   string `json:"password" cborgen:"password"`
}

type ServerCreateSession_Output struct {
	AccessJwt  string  `json:"accessJwt" cborgen:"accessJwt"`
	Did        string  `json:"did" cborgen:"did"`
	Email      *string `json:"email,omitempty" cborgen:"email,omitempty"`
	Handle     string  `json:"handle" cborgen:"handle"`
	RefreshJwt string  `json:"refreshJwt" cborgen:"refreshJwt"`
}

func ServerCreateSession(ctx context.Context, c *xrpc.Client, input *ServerCreateSession_Input) (*ServerCreateSession_Output, error) {
	var out ServerCreateSession_Output
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.server.createSession", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
