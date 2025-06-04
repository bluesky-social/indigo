// Copied from indigo:api/atproto/identitysubmitPlcOperation.go

package agnostic

// schema: com.atproto.identity.submitPlcOperation

import (
	"context"
	"encoding/json"

	"github.com/bluesky-social/indigo/lex/util"
)

// IdentitySubmitPlcOperation_Input is the input argument to a com.atproto.identity.submitPlcOperation call.
type IdentitySubmitPlcOperation_Input struct {
	Operation *json.RawMessage `json:"operation" cborgen:"operation"`
}

// IdentitySubmitPlcOperation calls the XRPC method "com.atproto.identity.submitPlcOperation".
func IdentitySubmitPlcOperation(ctx context.Context, c util.LexClient, input *IdentitySubmitPlcOperation_Input) error {
	if err := c.LexDo(ctx, util.Procedure, "application/json", "com.atproto.identity.submitPlcOperation", nil, input, nil); err != nil {
		return err
	}

	return nil
}
