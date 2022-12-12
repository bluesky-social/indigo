package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.session.get

type SessionGet_Output struct {
	Handle string `json:"handle" cborgen:"handle"`
	Did    string `json:"did" cborgen:"did"`
}

func (t *SessionGet_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["did"] = t.Did
	out["handle"] = t.Handle
	return json.Marshal(out)
}

func SessionGet(ctx context.Context, c *xrpc.Client) (*SessionGet_Output, error) {
	var out SessionGet_Output
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.session.get", nil, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
