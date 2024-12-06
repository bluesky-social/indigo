// Copied from indigo:api/atproto/identitygetRecommendedDidCredentials.go

package agnostic

// schema: com.atproto.identity.getRecommendedDidCredentials

import (
	"context"
	"encoding/json"

	"github.com/bluesky-social/indigo/xrpc"
)

// IdentityGetRecommendedDidCredentials calls the XRPC method "com.atproto.identity.getRecommendedDidCredentials".
func IdentityGetRecommendedDidCredentials(ctx context.Context, c *xrpc.Client) (*json.RawMessage, error) {
	var out json.RawMessage

	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.identity.getRecommendedDidCredentials", nil, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
