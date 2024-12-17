// Copied from indigo:api/atproto/repocreateRecords.go

package agnostic

// schema: com.atproto.repo.createRecord

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// RepoDefs_CommitMeta is a "commitMeta" in the com.atproto.repo.defs schema.
type RepoDefs_CommitMeta struct {
	Cid string `json:"cid" cborgen:"cid"`
	Rev string `json:"rev" cborgen:"rev"`
}

// RepoCreateRecord_Input is the input argument to a com.atproto.repo.createRecord call.
type RepoCreateRecord_Input struct {
	// collection: The NSID of the record collection.
	Collection string `json:"collection" cborgen:"collection"`
	// record: The record itself. Must contain a $type field.
	Record map[string]any `json:"record" cborgen:"record"`
	// repo: The handle or DID of the repo (aka, current account).
	Repo string `json:"repo" cborgen:"repo"`
	// rkey: The Record Key.
	Rkey *string `json:"rkey,omitempty" cborgen:"rkey,omitempty"`
	// swapCommit: Compare and swap with the previous commit by CID.
	SwapCommit *string `json:"swapCommit,omitempty" cborgen:"swapCommit,omitempty"`
	// validate: Can be set to 'false' to skip Lexicon schema validation of record data, 'true' to require it, or leave unset to validate only for known Lexicons.
	Validate *bool `json:"validate,omitempty" cborgen:"validate,omitempty"`
}

// RepoCreateRecord_Output is the output of a com.atproto.repo.createRecord call.
type RepoCreateRecord_Output struct {
	Cid              string               `json:"cid" cborgen:"cid"`
	Commit           *RepoDefs_CommitMeta `json:"commit,omitempty" cborgen:"commit,omitempty"`
	Uri              string               `json:"uri" cborgen:"uri"`
	ValidationStatus *string              `json:"validationStatus,omitempty" cborgen:"validationStatus,omitempty"`
}

// RepoCreateRecord calls the XRPC method "com.atproto.repo.createRecord".
func RepoCreateRecord(ctx context.Context, c *xrpc.Client, input *RepoCreateRecord_Input) (*RepoCreateRecord_Output, error) {
	var out RepoCreateRecord_Output
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.repo.createRecord", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
