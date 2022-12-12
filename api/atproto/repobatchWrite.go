package schemagen

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/whyrusleeping/gosky/lex/util"
	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.repo.batchWrite

type RepoBatchWrite_Input struct {
	Validate bool                                `json:"validate" cborgen:"validate"`
	Writes   []*RepoBatchWrite_Input_Writes_Elem `json:"writes" cborgen:"writes"`
	Did      string                              `json:"did" cborgen:"did"`
}

func (t *RepoBatchWrite_Input) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["did"] = t.Did
	out["validate"] = t.Validate
	out["writes"] = t.Writes
	return json.Marshal(out)
}

type RepoBatchWrite_Input_Writes_Elem struct {
	RepoBatchWrite_Create *RepoBatchWrite_Create
	RepoBatchWrite_Update *RepoBatchWrite_Update
	RepoBatchWrite_Delete *RepoBatchWrite_Delete
}

func (t *RepoBatchWrite_Input_Writes_Elem) MarshalJSON() ([]byte, error) {
	if t.RepoBatchWrite_Create != nil {
		return json.Marshal(t.RepoBatchWrite_Create)
	}
	if t.RepoBatchWrite_Update != nil {
		return json.Marshal(t.RepoBatchWrite_Update)
	}
	if t.RepoBatchWrite_Delete != nil {
		return json.Marshal(t.RepoBatchWrite_Delete)
	}
	return nil, fmt.Errorf("cannot marshal empty enum")
}
func (t *RepoBatchWrite_Input_Writes_Elem) UnmarshalJSON(b []byte) error {
	typ, err := util.EnumTypeExtract(b)
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

type RepoBatchWrite_Create struct {
	Action     string `json:"action" cborgen:"action"`
	Collection string `json:"collection" cborgen:"collection"`
	Rkey       string `json:"rkey" cborgen:"rkey"`
	Value      any    `json:"value" cborgen:"value"`
}

func (t *RepoBatchWrite_Create) MarshalJSON() ([]byte, error) {
	t.Action = "create"
	out := make(map[string]interface{})
	out["action"] = t.Action
	out["collection"] = t.Collection
	out["rkey"] = t.Rkey
	out["value"] = t.Value
	return json.Marshal(out)
}

type RepoBatchWrite_Update struct {
	Rkey       string `json:"rkey" cborgen:"rkey"`
	Value      any    `json:"value" cborgen:"value"`
	Action     string `json:"action" cborgen:"action"`
	Collection string `json:"collection" cborgen:"collection"`
}

func (t *RepoBatchWrite_Update) MarshalJSON() ([]byte, error) {
	t.Action = "update"
	out := make(map[string]interface{})
	out["action"] = t.Action
	out["collection"] = t.Collection
	out["rkey"] = t.Rkey
	out["value"] = t.Value
	return json.Marshal(out)
}

type RepoBatchWrite_Delete struct {
	Action     string `json:"action" cborgen:"action"`
	Collection string `json:"collection" cborgen:"collection"`
	Rkey       string `json:"rkey" cborgen:"rkey"`
}

func (t *RepoBatchWrite_Delete) MarshalJSON() ([]byte, error) {
	t.Action = "delete"
	out := make(map[string]interface{})
	out["action"] = t.Action
	out["collection"] = t.Collection
	out["rkey"] = t.Rkey
	return json.Marshal(out)
}

func RepoBatchWrite(ctx context.Context, c *xrpc.Client, input RepoBatchWrite_Input) error {
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.repo.batchWrite", nil, input, nil); err != nil {
		return err
	}

	return nil
}
