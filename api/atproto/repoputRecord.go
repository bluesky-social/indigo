package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.repo.putRecord

func init() {
}

type RepoPutRecord_Input struct {
	LexiconTypeID string                   `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Collection    string                   `json:"collection" cborgen:"collection"`
	Record        *util.LexiconTypeDecoder `json:"record" cborgen:"record"`
	Repo          string                   `json:"repo" cborgen:"repo"`
	Rkey          string                   `json:"rkey" cborgen:"rkey"`
	SwapCommit    *string                  `json:"swapCommit,omitempty" cborgen:"swapCommit"`
	SwapRecord    *string                  `json:"swapRecord" cborgen:"swapRecord"`
	Validate      *bool                    `json:"validate,omitempty" cborgen:"validate"`
}

type RepoPutRecord_Output struct {
	LexiconTypeID string `json:"$type,omitempty" cborgen:"$type,omitempty"`
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
