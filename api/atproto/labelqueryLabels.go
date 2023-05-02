// Code generated by cmd/lexgen (see Makefile's lexgen); DO NOT EDIT.

package atproto

// schema: com.atproto.label.queryLabels

import (
	"context"

	"github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/xrpc"
)

// LabelQueryLabels_Output is the output of a com.atproto.label.queryLabels call.
type LabelQueryLabels_Output struct {
	Cursor *string            `json:"cursor,omitempty" cborgen:"cursor,omitempty"`
	Labels []*LabelDefs_Label `json:"labels" cborgen:"labels"`
}

// LabelQueryLabels calls the XRPC method "com.atproto.label.queryLabels".
//
// sources: Optional list of label sources (DIDs) to filter on
// uriPatterns: List of AT URI patterns to match (boolean 'OR'). Each may be a prefix (ending with '*'; will match inclusive of the string leading to '*'), or a full URI
func LabelQueryLabels(ctx context.Context, c *xrpc.Client, cursor string, limit int64, sources []util.FormatDID, uriPatterns []string) (*LabelQueryLabels_Output, error) {
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
