package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.repo.createRecord

type RepoCreateRecord_Input struct {
	Did        string `json:"did" cborgen:"did"`
	Collection string `json:"collection" cborgen:"collection"`
	Validate   bool   `json:"validate" cborgen:"validate"`
	Record     any    `json:"record" cborgen:"record"`
}

func (t *RepoCreateRecord_Input) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["collection"] = t.Collection
	out["did"] = t.Did
	out["record"] = t.Record
	out["validate"] = t.Validate
	return json.Marshal(out)
}

type RepoCreateRecord_Output struct {
	Uri string `json:"uri" cborgen:"uri"`
	Cid string `json:"cid" cborgen:"cid"`
}

func (t *RepoCreateRecord_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["cid"] = t.Cid
	out["uri"] = t.Uri
	return json.Marshal(out)
}

func RepoCreateRecord(ctx context.Context, c *xrpc.Client, input RepoCreateRecord_Input) (*RepoCreateRecord_Output, error) {
	var out RepoCreateRecord_Output
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.repo.createRecord", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
