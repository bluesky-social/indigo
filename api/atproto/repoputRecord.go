package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.repo.putRecord

type RepoPutRecord_Input struct {
	Did        string `json:"did"`
	Collection string `json:"collection"`
	Rkey       string `json:"rkey"`
	Validate   bool   `json:"validate"`
	Record     any    `json:"record"`
}

func (t *RepoPutRecord_Input) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["collection"] = t.Collection
	out["did"] = t.Did
	out["record"] = t.Record
	out["rkey"] = t.Rkey
	out["validate"] = t.Validate
	return json.Marshal(out)
}

type RepoPutRecord_Output struct {
	Uri string `json:"uri"`
	Cid string `json:"cid"`
}

func (t *RepoPutRecord_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["cid"] = t.Cid
	out["uri"] = t.Uri
	return json.Marshal(out)
}

func RepoPutRecord(ctx context.Context, c *xrpc.Client, input RepoPutRecord_Input) (*RepoPutRecord_Output, error) {
	var out RepoPutRecord_Output
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.repo.putRecord", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
