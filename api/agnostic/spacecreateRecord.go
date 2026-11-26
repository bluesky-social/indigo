// Copied from indigo:api/atproto/spacecreateRecord.go

// Lexicon schema: com.atproto.space.createRecord

package agnostic

import (
	"context"
	"encoding/json"

	lexutil "github.com/bluesky-social/indigo/lex/util"
)

// SpaceCreateRecord_Input is the input argument to a com.atproto.space.createRecord call.
type SpaceCreateRecord_Input struct {
	Collection string `json:"collection" cborgen:"collection"`
	// record: The record itself. Must contain a $type field.
	Record *json.RawMessage `json:"record" cborgen:"record"`
	Repo   string           `json:"repo" cborgen:"repo"`
	Rkey   *string          `json:"rkey,omitempty" cborgen:"rkey,omitempty"`
	Space  string           `json:"space" cborgen:"space"`
	// validate: Can be set to 'false' to skip Lexicon schema validation of record data, 'true' to require it, or leave unset to validate only for known Lexicons.
	Validate *bool `json:"validate,omitempty" cborgen:"validate,omitempty"`
}

// SpaceCreateRecord_Output is the output of a com.atproto.space.createRecord call.
type SpaceCreateRecord_Output struct {
	Cid              string  `json:"cid" cborgen:"cid"`
	Uri              string  `json:"uri" cborgen:"uri"`
	ValidationStatus *string `json:"validationStatus,omitempty" cborgen:"validationStatus,omitempty"`
}

// SpaceCreateRecord calls the XRPC method "com.atproto.space.createRecord".
func SpaceCreateRecord(ctx context.Context, c lexutil.LexClient, input *SpaceCreateRecord_Input) (*SpaceCreateRecord_Output, error) {
	var out SpaceCreateRecord_Output
	if err := c.LexDo(ctx, lexutil.Procedure, "application/json", "com.atproto.space.createRecord", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
