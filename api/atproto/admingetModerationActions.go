package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.admin.getModerationActions

func init() {
}

type AdminGetModerationActions_Output struct {
	LexiconTypeID string                        `json:"$type,omitempty"`
	Actions       []*AdminModerationAction_View `json:"actions" cborgen:"actions"`
	Cursor        *string                       `json:"cursor,omitempty" cborgen:"cursor"`
}

func AdminGetModerationActions(ctx context.Context, c *xrpc.Client, before string, limit int64, subject string) (*AdminGetModerationActions_Output, error) {
	var out AdminGetModerationActions_Output

	params := map[string]interface{}{
		"before":  before,
		"limit":   limit,
		"subject": subject,
	}
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.admin.getModerationActions", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
