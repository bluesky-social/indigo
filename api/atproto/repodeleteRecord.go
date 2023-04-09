package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.repo.deleteRecord

func init() {
}

type RepoDeleteRecord_Input struct {
	Collection string  `json:"collection" cborgen:"collection"`
	Repo       string  `json:"repo" cborgen:"repo"`
	Rkey       string  `json:"rkey" cborgen:"rkey"`
	SwapCommit *string `json:"swapCommit,omitempty" cborgen:"swapCommit,omitempty"`
	SwapRecord *string `json:"swapRecord,omitempty" cborgen:"swapRecord,omitempty"`
}

func RepoDeleteRecord(ctx context.Context, c *xrpc.Client, input *RepoDeleteRecord_Input) error {
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.repo.deleteRecord", nil, input, nil); err != nil {
		return err
	}

	return nil
}
