package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.label.queryLabels

func init() {
}

type LabelQueryLabels_Output struct {
	Cursor *string            `json:"cursor,omitempty" cborgen:"cursor,omitempty"`
	Labels []*LabelDefs_Label `json:"labels" cborgen:"labels"`
}

func LabelQueryLabels(ctx context.Context, c *xrpc.Client, cursor string, limit int64, sources []string, uriPatterns []string) (*LabelQueryLabels_Output, error) {
	var out LabelQueryLabels_Output

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
