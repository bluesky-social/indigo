package atproto

import (
	"context"
	"io"

	"github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.repo.uploadBlob

func init() {
}

type RepoUploadBlob_Output struct {
	LexiconTypeID string        `json:"$type,omitempty"`
	Blob          *util.LexBlob `json:"blob" cborgen:"blob"`
}

func RepoUploadBlob(ctx context.Context, c *xrpc.Client, input io.Reader) (*RepoUploadBlob_Output, error) {
	var out RepoUploadBlob_Output
	if err := c.Do(ctx, xrpc.Procedure, "*/*", "com.atproto.repo.uploadBlob", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
