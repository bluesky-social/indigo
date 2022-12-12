package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.repo.deleteRecord

type RepoDeleteRecord_Input struct {
	Collection string `json:"collection" cborgen:"collection"`
	Rkey       string `json:"rkey" cborgen:"rkey"`
	Did        string `json:"did" cborgen:"did"`
}

func (t *RepoDeleteRecord_Input) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["collection"] = t.Collection
	out["did"] = t.Did
	out["rkey"] = t.Rkey
	return json.Marshal(out)
}

func RepoDeleteRecord(ctx context.Context, c *xrpc.Client, input RepoDeleteRecord_Input) error {
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.repo.deleteRecord", nil, input, nil); err != nil {
		return err
	}

	return nil
}
