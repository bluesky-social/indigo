package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.repo.describe

type RepoDescribe_Output struct {
	Handle          string   `json:"handle"`
	Did             string   `json:"did"`
	DidDoc          any      `json:"didDoc"`
	Collections     []string `json:"collections"`
	HandleIsCorrect bool     `json:"handleIsCorrect"`
}

func (t *RepoDescribe_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["collections"] = t.Collections
	out["did"] = t.Did
	out["didDoc"] = t.DidDoc
	out["handle"] = t.Handle
	out["handleIsCorrect"] = t.HandleIsCorrect
	return json.Marshal(out)
}

func RepoDescribe(ctx context.Context, c *xrpc.Client, user string) (*RepoDescribe_Output, error) {
	var out RepoDescribe_Output

	params := map[string]interface{}{
		"user": user,
	}
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.repo.describe", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
