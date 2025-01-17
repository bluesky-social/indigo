// Copied from indigo:api/atproto/identitysignPlcOperation.go

package agnostic

// schema: com.atproto.identity.signPlcOperation

import (
	"context"
	"encoding/json"

	"github.com/bluesky-social/indigo/xrpc"
)

// IdentitySignPlcOperation_Input is the input argument to a com.atproto.identity.signPlcOperation call.
type IdentitySignPlcOperation_Input struct {
	AlsoKnownAs  []string         `json:"alsoKnownAs,omitempty" cborgen:"alsoKnownAs,omitempty"`
	RotationKeys []string         `json:"rotationKeys,omitempty" cborgen:"rotationKeys,omitempty"`
	Services     *json.RawMessage `json:"services,omitempty" cborgen:"services,omitempty"`
	// token: A token received through com.atproto.identity.requestPlcOperationSignature
	Token               *string          `json:"token,omitempty" cborgen:"token,omitempty"`
	VerificationMethods *json.RawMessage `json:"verificationMethods,omitempty" cborgen:"verificationMethods,omitempty"`
}

// IdentitySignPlcOperation_Output is the output of a com.atproto.identity.signPlcOperation call.
type IdentitySignPlcOperation_Output struct {
	// operation: A signed DID PLC operation.
	Operation *json.RawMessage `json:"operation" cborgen:"operation"`
}

// IdentitySignPlcOperation calls the XRPC method "com.atproto.identity.signPlcOperation".
func IdentitySignPlcOperation(ctx context.Context, c *xrpc.Client, input *IdentitySignPlcOperation_Input) (*IdentitySignPlcOperation_Output, error) {
	var out IdentitySignPlcOperation_Output
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.identity.signPlcOperation", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
