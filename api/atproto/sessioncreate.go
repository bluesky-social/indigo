package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.session.create

type SessionCreate_Input struct {
	Handle   string `json:"handle"`
	Password string `json:"password"`
}

func (t *SessionCreate_Input) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["handle"] = t.Handle
	out["password"] = t.Password
	return json.Marshal(out)
}

type SessionCreate_Output struct {
	AccessJwt  string `json:"accessJwt"`
	RefreshJwt string `json:"refreshJwt"`
	Handle     string `json:"handle"`
	Did        string `json:"did"`
}

func (t *SessionCreate_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["accessJwt"] = t.AccessJwt
	out["did"] = t.Did
	out["handle"] = t.Handle
	out["refreshJwt"] = t.RefreshJwt
	return json.Marshal(out)
}

func SessionCreate(ctx context.Context, c *xrpc.Client, input SessionCreate_Input) (*SessionCreate_Output, error) {
	var out SessionCreate_Output
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.session.create", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
