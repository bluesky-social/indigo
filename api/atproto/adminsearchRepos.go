package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.admin.searchRepos

func init() {
}

type AdminSearchRepos_Output struct {
	LexiconTypeID string                `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Cursor        *string               `json:"cursor,omitempty" cborgen:"cursor"`
	Repos         []*AdminDefs_RepoView `json:"repos" cborgen:"repos"`
}

func AdminSearchRepos(ctx context.Context, c *xrpc.Client, cursor string, limit int64, term string) (*AdminSearchRepos_Output, error) {
	var out AdminSearchRepos_Output

	params := map[string]interface{}{
		"cursor": cursor,
		"limit":  limit,
		"term":   term,
	}
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.admin.searchRepos", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
