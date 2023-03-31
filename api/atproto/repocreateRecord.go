package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.repo.createRecord

func init() {
}

type RepoCreateRecord_Input struct {
	LexiconTypeID string                   `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Collection    string                   `json:"collection" cborgen:"collection"`
	Record        *util.LexiconTypeDecoder `json:"record" cborgen:"record"`
	Repo          string                   `json:"repo" cborgen:"repo"`
	Rkey          *string                  `json:"rkey,omitempty" cborgen:"rkey,omitempty"`
	SwapCommit    *string                  `json:"swapCommit,omitempty" cborgen:"swapCommit,omitempty"`
	Validate      *bool                    `json:"validate,omitempty" cborgen:"validate,omitempty"`
}

type RepoCreateRecord_Output struct {
	LexiconTypeID string `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Cid           string `json:"cid" cborgen:"cid"`
	Uri           string `json:"uri" cborgen:"uri"`
}

func RepoCreateRecord(ctx context.Context, c *xrpc.Client, input *RepoCreateRecord_Input) (*RepoCreateRecord_Output, error) {
	var out RepoCreateRecord_Output
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.repo.createRecord", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
