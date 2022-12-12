package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.repo.getRecord

type RepoGetRecord_Output struct {
	Value any    `json:"value" cborgen:"value"`
	Uri   string `json:"uri" cborgen:"uri"`
	Cid   string `json:"cid" cborgen:"cid"`
}

func (t *RepoGetRecord_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["cid"] = t.Cid
	out["uri"] = t.Uri
	out["value"] = t.Value
	return json.Marshal(out)
}

func RepoGetRecord(ctx context.Context, c *xrpc.Client, cid string, collection string, rkey string, user string) (*RepoGetRecord_Output, error) {
	var out RepoGetRecord_Output

	params := map[string]interface{}{
		"cid":        cid,
		"collection": collection,
		"rkey":       rkey,
		"user":       user,
	}
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.repo.getRecord", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
