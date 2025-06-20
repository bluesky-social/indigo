// Copied from indigo:api/atproto/identitygetRecommendedDidCredentials.go

package agnostic

// schema: com.atproto.identity.getRecommendedDidCredentials

import (
	"context"
	"encoding/json"

	"github.com/gander-social/gander-indigo-sovereign/lex/util"
)

// IdentityGetRecommendedDidCredentials calls the XRPC method "com.atproto.identity.getRecommendedDidCredentials".
func IdentityGetRecommendedDidCredentials(ctx context.Context, c util.LexClient) (*json.RawMessage, error) {
	var out json.RawMessage

	if err := c.LexDo(ctx, util.Query, "", "com.atproto.identity.getRecommendedDidCredentials", nil, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
