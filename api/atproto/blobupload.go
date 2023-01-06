package schemagen

import (
	"context"
	"io"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.blob.upload

func init() {
}

type BlobUpload_Output struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Cid           string `json:"cid" cborgen:"cid"`
}

func BlobUpload(ctx context.Context, c *xrpc.Client, input io.Reader) (*BlobUpload_Output, error) {
	var out BlobUpload_Output
	if err := c.Do(ctx, xrpc.Procedure, "*/*", "com.atproto.blob.upload", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
