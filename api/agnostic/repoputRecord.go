// Copied from indigo:api/atproto/repoputRecords.go

package agnostic

// schema: com.atproto.repo.putRecord

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// RepoPutRecord_Input is the input argument to a com.atproto.repo.putRecord call.
type RepoPutRecord_Input struct {
	// collection: The NSID of the record collection.
	Collection string `json:"collection" cborgen:"collection"`
	// record: The record to write.
	Record map[string]any `json:"record" cborgen:"record"`
	// repo: The handle or DID of the repo (aka, current account).
	Repo string `json:"repo" cborgen:"repo"`
	// rkey: The Record Key.
	Rkey string `json:"rkey" cborgen:"rkey"`
	// swapCommit: Compare and swap with the previous commit by CID.
	SwapCommit *string `json:"swapCommit,omitempty" cborgen:"swapCommit,omitempty"`
	// swapRecord: Compare and swap with the previous record by CID. WARNING: nullable and optional field; may cause problems with golang implementation
	SwapRecord *string `json:"swapRecord" cborgen:"swapRecord"`
	// validate: Can be set to 'false' to skip Lexicon schema validation of record data, 'true' to require it, or leave unset to validate only for known Lexicons.
	Validate *bool `json:"validate,omitempty" cborgen:"validate,omitempty"`
}

// RepoPutRecord_Output is the output of a com.atproto.repo.putRecord call.
type RepoPutRecord_Output struct {
	Cid              string               `json:"cid" cborgen:"cid"`
	Commit           *RepoDefs_CommitMeta `json:"commit,omitempty" cborgen:"commit,omitempty"`
	Uri              string               `json:"uri" cborgen:"uri"`
	ValidationStatus *string              `json:"validationStatus,omitempty" cborgen:"validationStatus,omitempty"`
}

// RepoPutRecord calls the XRPC method "com.atproto.repo.putRecord".
func RepoPutRecord(ctx context.Context, c *xrpc.Client, input *RepoPutRecord_Input) (*RepoPutRecord_Output, error) {
	var out RepoPutRecord_Output
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.repo.putRecord", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
