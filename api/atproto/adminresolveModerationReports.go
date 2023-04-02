package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.admin.resolveModerationReports

func init() {
}

type AdminResolveModerationReports_Input struct {
	LexiconTypeID string  `json:"$type,omitempty" cborgen:"$type,omitempty"`
	ActionId      int64   `json:"actionId" cborgen:"actionId"`
	CreatedBy     string  `json:"createdBy" cborgen:"createdBy"`
	ReportIds     []int64 `json:"reportIds" cborgen:"reportIds"`
}

func AdminResolveModerationReports(ctx context.Context, c *xrpc.Client, input *AdminResolveModerationReports_Input) (*AdminDefs_ActionView, error) {
	var out AdminDefs_ActionView
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.admin.resolveModerationReports", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
