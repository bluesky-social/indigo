package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.repo.putRecord

func init() {
}

type RepoPutRecord_Input struct {
	Did        string `json:"did" cborgen:"did"`
	Collection string `json:"collection" cborgen:"collection"`
	Rkey       string `json:"rkey" cborgen:"rkey"`
	Validate   *bool  `json:"validate" cborgen:"validate"`
	Record     any    `json:"record" cborgen:"record"`
}

type RepoPutRecord_Output struct {
	Uri string `json:"uri" cborgen:"uri"`
	Cid string `json:"cid" cborgen:"cid"`
}

func RepoPutRecord(ctx context.Context, c *xrpc.Client, input RepoPutRecord_Input) (*RepoPutRecord_Output, error) {
	var out RepoPutRecord_Output
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.repo.putRecord", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
