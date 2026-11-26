// Copied from indigo:api/atproto/spaceputRecord.go

// Lexicon schema: com.atproto.space.putRecord

package agnostic

import (
	"context"
	"encoding/json"

	lexutil "github.com/bluesky-social/indigo/lex/util"
)

// SpacePutRecord_Input is the input argument to a com.atproto.space.putRecord call.
type SpacePutRecord_Input struct {
	Collection string           `json:"collection" cborgen:"collection"`
	Record     *json.RawMessage `json:"record" cborgen:"record"`
	Repo       string           `json:"repo" cborgen:"repo"`
	Rkey       string           `json:"rkey" cborgen:"rkey"`
	Space      string           `json:"space" cborgen:"space"`
	// validate: Can be set to 'false' to skip Lexicon schema validation of record data, 'true' to require it, or leave unset to validate only for known Lexicons.
	Validate *bool `json:"validate,omitempty" cborgen:"validate,omitempty"`
}

// SpacePutRecord_Output is the output of a com.atproto.space.putRecord call.
type SpacePutRecord_Output struct {
	Cid              string  `json:"cid" cborgen:"cid"`
	Uri              string  `json:"uri" cborgen:"uri"`
	ValidationStatus *string `json:"validationStatus,omitempty" cborgen:"validationStatus,omitempty"`
}

// SpacePutRecord calls the XRPC method "com.atproto.space.putRecord".
func SpacePutRecord(ctx context.Context, c lexutil.LexClient, input *SpacePutRecord_Input) (*SpacePutRecord_Output, error) {
	var out SpacePutRecord_Output
	if err := c.LexDo(ctx, lexutil.Procedure, "application/json", "com.atproto.space.putRecord", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
