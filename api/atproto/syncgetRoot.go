package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.sync.getRoot

type SyncGetRoot_Output struct {
	Root string `json:"root" cborgen:"root"`
}

func (t *SyncGetRoot_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["root"] = t.Root
	return json.Marshal(out)
}

func SyncGetRoot(ctx context.Context, c *xrpc.Client, did string) (*SyncGetRoot_Output, error) {
	var out SyncGetRoot_Output

	params := map[string]interface{}{
		"did": did,
	}
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.sync.getRoot", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
