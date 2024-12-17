// Copied from indigo:api/atproto/repoapplyWrites.go

package agnostic

// schema: com.atproto.repo.applyWrites

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/xrpc"
)

// RepoApplyWrites_Create is a "create" in the com.atproto.repo.applyWrites schema.
//
// Operation which creates a new record.
//
// RECORDTYPE: RepoApplyWrites_Create
type RepoApplyWrites_Create struct {
	LexiconTypeID string           `json:"$type,const=com.atproto.repo.applyWrites#create" cborgen:"$type,const=com.atproto.repo.applyWrites#create"`
	Collection    string           `json:"collection" cborgen:"collection"`
	Rkey          *string          `json:"rkey,omitempty" cborgen:"rkey,omitempty"`
	Value         *json.RawMessage `json:"value" cborgen:"value"`
}

// RepoApplyWrites_CreateResult is a "createResult" in the com.atproto.repo.applyWrites schema.
//
// RECORDTYPE: RepoApplyWrites_CreateResult
type RepoApplyWrites_CreateResult struct {
	LexiconTypeID    string  `json:"$type,const=com.atproto.repo.applyWrites#createResult" cborgen:"$type,const=com.atproto.repo.applyWrites#createResult"`
	Cid              string  `json:"cid" cborgen:"cid"`
	Uri              string  `json:"uri" cborgen:"uri"`
	ValidationStatus *string `json:"validationStatus,omitempty" cborgen:"validationStatus,omitempty"`
}

// RepoApplyWrites_Delete is a "delete" in the com.atproto.repo.applyWrites schema.
//
// Operation which deletes an existing record.
//
// RECORDTYPE: RepoApplyWrites_Delete
type RepoApplyWrites_Delete struct {
	LexiconTypeID string `json:"$type,const=com.atproto.repo.applyWrites#delete" cborgen:"$type,const=com.atproto.repo.applyWrites#delete"`
	Collection    string `json:"collection" cborgen:"collection"`
	Rkey          string `json:"rkey" cborgen:"rkey"`
}

// RepoApplyWrites_DeleteResult is a "deleteResult" in the com.atproto.repo.applyWrites schema.
//
// RECORDTYPE: RepoApplyWrites_DeleteResult
type RepoApplyWrites_DeleteResult struct {
	LexiconTypeID string `json:"$type,const=com.atproto.repo.applyWrites#deleteResult" cborgen:"$type,const=com.atproto.repo.applyWrites#deleteResult"`
}

// RepoApplyWrites_Input is the input argument to a com.atproto.repo.applyWrites call.
type RepoApplyWrites_Input struct {
	// repo: The handle or DID of the repo (aka, current account).
	Repo string `json:"repo" cborgen:"repo"`
	// swapCommit: If provided, the entire operation will fail if the current repo commit CID does not match this value. Used to prevent conflicting repo mutations.
	SwapCommit *string `json:"swapCommit,omitempty" cborgen:"swapCommit,omitempty"`
	// validate: Can be set to 'false' to skip Lexicon schema validation of record data across all operations, 'true' to require it, or leave unset to validate only for known Lexicons.
	Validate *bool                                `json:"validate,omitempty" cborgen:"validate,omitempty"`
	Writes   []*RepoApplyWrites_Input_Writes_Elem `json:"writes" cborgen:"writes"`
}

type RepoApplyWrites_Input_Writes_Elem struct {
	RepoApplyWrites_Create *RepoApplyWrites_Create
	RepoApplyWrites_Update *RepoApplyWrites_Update
	RepoApplyWrites_Delete *RepoApplyWrites_Delete
}

func (t *RepoApplyWrites_Input_Writes_Elem) MarshalJSON() ([]byte, error) {
	if t.RepoApplyWrites_Create != nil {
		t.RepoApplyWrites_Create.LexiconTypeID = "com.atproto.repo.applyWrites#create"
		return json.Marshal(t.RepoApplyWrites_Create)
	}
	if t.RepoApplyWrites_Update != nil {
		t.RepoApplyWrites_Update.LexiconTypeID = "com.atproto.repo.applyWrites#update"
		return json.Marshal(t.RepoApplyWrites_Update)
	}
	if t.RepoApplyWrites_Delete != nil {
		t.RepoApplyWrites_Delete.LexiconTypeID = "com.atproto.repo.applyWrites#delete"
		return json.Marshal(t.RepoApplyWrites_Delete)
	}
	return nil, fmt.Errorf("cannot marshal empty enum")
}
func (t *RepoApplyWrites_Input_Writes_Elem) UnmarshalJSON(b []byte) error {
	typ, err := util.TypeExtract(b)
	if err != nil {
		return err
	}

	switch typ {
	case "com.atproto.repo.applyWrites#create":
		t.RepoApplyWrites_Create = new(RepoApplyWrites_Create)
		return json.Unmarshal(b, t.RepoApplyWrites_Create)
	case "com.atproto.repo.applyWrites#update":
		t.RepoApplyWrites_Update = new(RepoApplyWrites_Update)
		return json.Unmarshal(b, t.RepoApplyWrites_Update)
	case "com.atproto.repo.applyWrites#delete":
		t.RepoApplyWrites_Delete = new(RepoApplyWrites_Delete)
		return json.Unmarshal(b, t.RepoApplyWrites_Delete)

	default:
		return fmt.Errorf("closed enums must have a matching value")
	}
}

// RepoApplyWrites_Output is the output of a com.atproto.repo.applyWrites call.
type RepoApplyWrites_Output struct {
	Commit  *RepoDefs_CommitMeta                   `json:"commit,omitempty" cborgen:"commit,omitempty"`
	Results []*RepoApplyWrites_Output_Results_Elem `json:"results,omitempty" cborgen:"results,omitempty"`
}

type RepoApplyWrites_Output_Results_Elem struct {
	RepoApplyWrites_CreateResult *RepoApplyWrites_CreateResult
	RepoApplyWrites_UpdateResult *RepoApplyWrites_UpdateResult
	RepoApplyWrites_DeleteResult *RepoApplyWrites_DeleteResult
}

func (t *RepoApplyWrites_Output_Results_Elem) MarshalJSON() ([]byte, error) {
	if t.RepoApplyWrites_CreateResult != nil {
		t.RepoApplyWrites_CreateResult.LexiconTypeID = "com.atproto.repo.applyWrites#createResult"
		return json.Marshal(t.RepoApplyWrites_CreateResult)
	}
	if t.RepoApplyWrites_UpdateResult != nil {
		t.RepoApplyWrites_UpdateResult.LexiconTypeID = "com.atproto.repo.applyWrites#updateResult"
		return json.Marshal(t.RepoApplyWrites_UpdateResult)
	}
	if t.RepoApplyWrites_DeleteResult != nil {
		t.RepoApplyWrites_DeleteResult.LexiconTypeID = "com.atproto.repo.applyWrites#deleteResult"
		return json.Marshal(t.RepoApplyWrites_DeleteResult)
	}
	return nil, fmt.Errorf("cannot marshal empty enum")
}
func (t *RepoApplyWrites_Output_Results_Elem) UnmarshalJSON(b []byte) error {
	typ, err := util.TypeExtract(b)
	if err != nil {
		return err
	}

	switch typ {
	case "com.atproto.repo.applyWrites#createResult":
		t.RepoApplyWrites_CreateResult = new(RepoApplyWrites_CreateResult)
		return json.Unmarshal(b, t.RepoApplyWrites_CreateResult)
	case "com.atproto.repo.applyWrites#updateResult":
		t.RepoApplyWrites_UpdateResult = new(RepoApplyWrites_UpdateResult)
		return json.Unmarshal(b, t.RepoApplyWrites_UpdateResult)
	case "com.atproto.repo.applyWrites#deleteResult":
		t.RepoApplyWrites_DeleteResult = new(RepoApplyWrites_DeleteResult)
		return json.Unmarshal(b, t.RepoApplyWrites_DeleteResult)

	default:
		return fmt.Errorf("closed enums must have a matching value")
	}
}

// RepoApplyWrites_Update is a "update" in the com.atproto.repo.applyWrites schema.
//
// Operation which updates an existing record.
//
// RECORDTYPE: RepoApplyWrites_Update
type RepoApplyWrites_Update struct {
	LexiconTypeID string           `json:"$type,const=com.atproto.repo.applyWrites#update" cborgen:"$type,const=com.atproto.repo.applyWrites#update"`
	Collection    string           `json:"collection" cborgen:"collection"`
	Rkey          string           `json:"rkey" cborgen:"rkey"`
	Value         *json.RawMessage `json:"value" cborgen:"value"`
}

// RepoApplyWrites_UpdateResult is a "updateResult" in the com.atproto.repo.applyWrites schema.
//
// RECORDTYPE: RepoApplyWrites_UpdateResult
type RepoApplyWrites_UpdateResult struct {
	LexiconTypeID    string  `json:"$type,const=com.atproto.repo.applyWrites#updateResult" cborgen:"$type,const=com.atproto.repo.applyWrites#updateResult"`
	Cid              string  `json:"cid" cborgen:"cid"`
	Uri              string  `json:"uri" cborgen:"uri"`
	ValidationStatus *string `json:"validationStatus,omitempty" cborgen:"validationStatus,omitempty"`
}

// RepoApplyWrites calls the XRPC method "com.atproto.repo.applyWrites".
func RepoApplyWrites(ctx context.Context, c *xrpc.Client, input *RepoApplyWrites_Input) (*RepoApplyWrites_Output, error) {
	var out RepoApplyWrites_Output
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.repo.applyWrites", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
