package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.server.createSession

func init() {
}

type ServerCreateSession_Input struct {
	LexiconTypeID string  `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Identifier    *string `json:"identifier,omitempty" cborgen:"identifier,omitempty"`
	Password      string  `json:"password" cborgen:"password"`
}

type ServerCreateSession_Output struct {
	LexiconTypeID string `json:"$type,omitempty" cborgen:"$type,omitempty"`
	AccessJwt     string `json:"accessJwt" cborgen:"accessJwt"`
	Did           string `json:"did" cborgen:"did"`
	Handle        string `json:"handle" cborgen:"handle"`
	RefreshJwt    string `json:"refreshJwt" cborgen:"refreshJwt"`
}

func ServerCreateSession(ctx context.Context, c *xrpc.Client, input *ServerCreateSession_Input) (*ServerCreateSession_Output, error) {
	var out ServerCreateSession_Output
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.server.createSession", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
