package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.repo.listRecords

func init() {
}

type RepoListRecords_Output struct {
	Cursor  *string                   `json:"cursor" cborgen:"cursor"`
	Records []*RepoListRecords_Record `json:"records" cborgen:"records"`
}

type RepoListRecords_Record struct {
	Uri   string `json:"uri" cborgen:"uri"`
	Cid   string `json:"cid" cborgen:"cid"`
	Value any    `json:"value" cborgen:"value"`
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
