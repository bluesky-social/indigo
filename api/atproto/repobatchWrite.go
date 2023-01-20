package schemagen

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.repo.batchWrite

func init() {
}

type RepoBatchWrite_Create struct {
	LexiconTypeID string                  `json:"$type,omitempty"`
	Action        string                  `json:"action" cborgen:"action"`
	Collection    string                  `json:"collection" cborgen:"collection"`
	Rkey          *string                 `json:"rkey,omitempty" cborgen:"rkey"`
	Value         util.LexiconTypeDecoder `json:"value" cborgen:"value"`
}

type RepoBatchWrite_Delete struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Action        string `json:"action" cborgen:"action"`
	Collection    string `json:"collection" cborgen:"collection"`
	Rkey          string `json:"rkey" cborgen:"rkey"`
}

type RepoBatchWrite_Input struct {
	LexiconTypeID string                              `json:"$type,omitempty"`
	Did           string                              `json:"did" cborgen:"did"`
	Validate      *bool                               `json:"validate,omitempty" cborgen:"validate"`
	Writes        []*RepoBatchWrite_Input_Writes_Elem `json:"writes" cborgen:"writes"`
}

type RepoBatchWrite_Input_Writes_Elem struct {
	RepoBatchWrite_Create *RepoBatchWrite_Create
	RepoBatchWrite_Update *RepoBatchWrite_Update
	RepoBatchWrite_Delete *RepoBatchWrite_Delete
}

func (t *RepoBatchWrite_Input_Writes_Elem) MarshalJSON() ([]byte, error) {
	if t.RepoBatchWrite_Create != nil {
		t.RepoBatchWrite_Create.LexiconTypeID = "com.atproto.repo.batchWrite#create"
		return json.Marshal(t.RepoBatchWrite_Create)
	}
	if t.RepoBatchWrite_Update != nil {
		t.RepoBatchWrite_Update.LexiconTypeID = "com.atproto.repo.batchWrite#update"
		return json.Marshal(t.RepoBatchWrite_Update)
	}
	if t.RepoBatchWrite_Delete != nil {
		t.RepoBatchWrite_Delete.LexiconTypeID = "com.atproto.repo.batchWrite#delete"
		return json.Marshal(t.RepoBatchWrite_Delete)
	}
	return nil, fmt.Errorf("cannot marshal empty enum")
}
func (t *RepoBatchWrite_Input_Writes_Elem) UnmarshalJSON(b []byte) error {
	typ, err := util.TypeExtract(b)
	if err != nil {
		return err
	}

	switch typ {
	case "com.atproto.repo.batchWrite#create":
		t.RepoBatchWrite_Create = new(RepoBatchWrite_Create)
		return json.Unmarshal(b, t.RepoBatchWrite_Create)
	case "com.atproto.repo.batchWrite#update":
		t.RepoBatchWrite_Update = new(RepoBatchWrite_Update)
		return json.Unmarshal(b, t.RepoBatchWrite_Update)
	case "com.atproto.repo.batchWrite#delete":
		t.RepoBatchWrite_Delete = new(RepoBatchWrite_Delete)
		return json.Unmarshal(b, t.RepoBatchWrite_Delete)

	default:
		return fmt.Errorf("closed enums must have a matching value")
	}
}

type RepoBatchWrite_Update struct {
	LexiconTypeID string                  `json:"$type,omitempty"`
	Action        string                  `json:"action" cborgen:"action"`
	Collection    string                  `json:"collection" cborgen:"collection"`
	Rkey          string                  `json:"rkey" cborgen:"rkey"`
	Value         util.LexiconTypeDecoder `json:"value" cborgen:"value"`
}

func RepoBatchWrite(ctx context.Context, c *xrpc.Client, input *RepoBatchWrite_Input) error {
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.repo.batchWrite", nil, input, nil); err != nil {
		return err
	}

	return nil
}
