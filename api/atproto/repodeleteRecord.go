package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.repo.deleteRecord

func init() {
}

type RepoDeleteRecord_Input struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Collection    string `json:"collection" cborgen:"collection"`
	Did           string `json:"did" cborgen:"did"`
	Rkey          string `json:"rkey" cborgen:"rkey"`
}

func RepoDeleteRecord(ctx context.Context, c *xrpc.Client, input *RepoDeleteRecord_Input) error {
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.repo.deleteRecord", nil, input, nil); err != nil {
		return err
	}

	return nil
}
