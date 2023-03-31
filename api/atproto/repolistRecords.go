package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.repo.listRecords

func init() {
}

type RepoListRecords_Output struct {
	LexiconTypeID string                    `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Cursor        *string                   `json:"cursor,omitempty" cborgen:"cursor"`
	Records       []*RepoListRecords_Record `json:"records" cborgen:"records"`
}

type RepoListRecords_Record struct {
	LexiconTypeID string                   `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Cid           string                   `json:"cid" cborgen:"cid"`
	Uri           string                   `json:"uri" cborgen:"uri"`
	Value         *util.LexiconTypeDecoder `json:"value" cborgen:"value"`
}

func RepoListRecords(ctx context.Context, c *xrpc.Client, collection string, limit int64, repo string, reverse bool, rkeyEnd string, rkeyStart string) (*RepoListRecords_Output, error) {
	var out RepoListRecords_Output

	params := map[string]interface{}{
		"collection": collection,
		"limit":      limit,
		"repo":       repo,
		"reverse":    reverse,
		"rkeyEnd":    rkeyEnd,
		"rkeyStart":  rkeyStart,
	}
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.repo.listRecords", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
