package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.admin.getModerationReports

func init() {
}

type AdminGetModerationReports_Output struct {
	Cursor  *string                 `json:"cursor,omitempty" cborgen:"cursor,omitempty"`
	Reports []*AdminDefs_ReportView `json:"reports" cborgen:"reports"`
}

func AdminGetModerationReports(ctx context.Context, c *xrpc.Client, cursor string, limit int64, resolved bool, subject string) (*AdminGetModerationReports_Output, error) {
	var out AdminGetModerationReports_Output

	params := map[string]interface{}{
		"cursor":   cursor,
		"limit":    limit,
		"resolved": resolved,
		"subject":  subject,
	}
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.admin.getModerationReports", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
