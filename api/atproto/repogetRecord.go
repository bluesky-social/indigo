package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/lex/util"
	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.repo.getRecord

func init() {
}

type RepoGetRecord_Output struct {
	LexiconTypeID string                  `json:"$type,omitempty"`
	Cid           *string                 `json:"cid,omitempty" cborgen:"cid"`
	Uri           string                  `json:"uri" cborgen:"uri"`
	Value         util.LexiconTypeDecoder `json:"value" cborgen:"value"`
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
