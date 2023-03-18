package label

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.label.query

func init() {
}

type Query_Output struct {
	LexiconTypeID string   `json:"$type,omitempty"`
	Cursor        *string  `json:"cursor,omitempty" cborgen:"cursor"`
	Labels        []*Label `json:"labels" cborgen:"labels"`
}

func Query(ctx context.Context, c *xrpc.Client, cursor string, limit int64, sources []string, uriPatterns []string) (*Query_Output, error) {
	var out Query_Output

	params := map[string]interface{}{
		"cursor":      cursor,
		"limit":       limit,
		"sources":     sources,
		"uriPatterns": uriPatterns,
	}
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.label.query", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
