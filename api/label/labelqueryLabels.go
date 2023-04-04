package label

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.label.queryLabels

func init() {
}

type QueryLabels_Output struct {
	LexiconTypeID string   `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Cursor        *string  `json:"cursor,omitempty" cborgen:"cursor,omitempty"`
	Labels        []*Label `json:"labels" cborgen:"labels"`
}

func QueryLabels(ctx context.Context, c *xrpc.Client, cursor string, limit int64, sources []string, uriPatterns []string) (*QueryLabels_Output, error) {
	var out QueryLabels_Output

	params := map[string]interface{}{
		"cursor":      cursor,
		"limit":       limit,
		"sources":     sources,
		"uriPatterns": uriPatterns,
	}
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.label.queryLabels", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
