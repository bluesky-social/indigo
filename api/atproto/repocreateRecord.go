package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.repo.createRecord

func init() {
}

type RepoCreateRecord_Input struct {
	Did        string `json:"did" cborgen:"did"`
	Collection string `json:"collection" cborgen:"collection"`
	Validate   *bool  `json:"validate" cborgen:"validate"`
	Record     any    `json:"record" cborgen:"record"`
}

type RepoCreateRecord_Output struct {
	Cid string `json:"cid" cborgen:"cid"`
	Uri string `json:"uri" cborgen:"uri"`
}

func RepoCreateRecord(ctx context.Context, c *xrpc.Client, input RepoCreateRecord_Input) (*RepoCreateRecord_Output, error) {
	var out RepoCreateRecord_Output
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.repo.createRecord", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
