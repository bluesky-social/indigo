package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.repo.listRecords

type RepoListRecords_Output struct {
	Cursor  string                    `json:"cursor" cborgen:"cursor"`
	Records []*RepoListRecords_Record `json:"records" cborgen:"records"`
}

func (t *RepoListRecords_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["cursor"] = t.Cursor
	out["records"] = t.Records
	return json.Marshal(out)
}

type RepoListRecords_Record struct {
	Value any    `json:"value" cborgen:"value"`
	Uri   string `json:"uri" cborgen:"uri"`
	Cid   string `json:"cid" cborgen:"cid"`
}

func (t *RepoListRecords_Record) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["cid"] = t.Cid
	out["uri"] = t.Uri
	out["value"] = t.Value
	return json.Marshal(out)
}

func RepoListRecords(ctx context.Context, c *xrpc.Client, after string, before string, collection string, limit int64, reverse bool, user string) (*RepoListRecords_Output, error) {
	var out RepoListRecords_Output

	params := map[string]interface{}{
		"after":      after,
		"before":     before,
		"collection": collection,
		"limit":      limit,
		"reverse":    reverse,
		"user":       user,
	}
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.repo.listRecords", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
