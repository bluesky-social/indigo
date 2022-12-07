package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.handle.resolve

type HandleResolve_Output struct {
	Did string `json:"did"`
}

func (t *HandleResolve_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["did"] = t.Did
	return json.Marshal(out)
}

func HandleResolve(ctx context.Context, c *xrpc.Client, handle string) (*HandleResolve_Output, error) {
	var out HandleResolve_Output

	params := map[string]interface{}{
		"handle": handle,
	}
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.handle.resolve", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
