package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.sync.getCommitPath

func init() {
}

type SyncGetCommitPath_Output struct {
	LexiconTypeID string   `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Commits       []string `json:"commits" cborgen:"commits"`
}

func SyncGetCommitPath(ctx context.Context, c *xrpc.Client, did string, earliest string, latest string) (*SyncGetCommitPath_Output, error) {
	var out SyncGetCommitPath_Output

	params := map[string]interface{}{
		"did":      did,
		"earliest": earliest,
		"latest":   latest,
	}
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.sync.getCommitPath", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
