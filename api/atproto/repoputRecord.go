package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.repo.putRecord

func init() {
}

type RepoPutRecord_Input struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Collection    string `json:"collection" cborgen:"collection"`
	Did           string `json:"did" cborgen:"did"`
	Record        any    `json:"record" cborgen:"record"`
	Rkey          string `json:"rkey" cborgen:"rkey"`
	Validate      *bool  `json:"validate,omitempty" cborgen:"validate"`
}

type RepoPutRecord_Output struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Cid           string `json:"cid" cborgen:"cid"`
	Uri           string `json:"uri" cborgen:"uri"`
}

func RepoPutRecord(ctx context.Context, c *xrpc.Client, input *RepoPutRecord_Input) (*RepoPutRecord_Output, error) {
	var out RepoPutRecord_Output
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.repo.putRecord", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
