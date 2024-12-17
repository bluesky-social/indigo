// Copied from indigo:api/atproto/repolistRecords.go

package agnostic

// schema: com.atproto.repo.listRecords

import (
	"context"
	"encoding/json"

	"github.com/bluesky-social/indigo/xrpc"
)

// RepoListRecords_Output is the output of a com.atproto.repo.listRecords call.
type RepoListRecords_Output struct {
	Cursor  *string                   `json:"cursor,omitempty" cborgen:"cursor,omitempty"`
	Records []*RepoListRecords_Record `json:"records" cborgen:"records"`
}

// RepoListRecords_Record is a "record" in the com.atproto.repo.listRecords schema.
type RepoListRecords_Record struct {
	Cid string `json:"cid" cborgen:"cid"`
	Uri string `json:"uri" cborgen:"uri"`
	//  NOTE: changed from lex decoder to json.RawMessage
	Value *json.RawMessage `json:"value" cborgen:"value"`
}

// RepoListRecords calls the XRPC method "com.atproto.repo.listRecords".
//
// collection: The NSID of the record type.
// limit: The number of records to return.
// repo: The handle or DID of the repo.
// reverse: Flag to reverse the order of the returned records.
// rkeyEnd: DEPRECATED: The highest sort-ordered rkey to stop at (exclusive)
// rkeyStart: DEPRECATED: The lowest sort-ordered rkey to start from (exclusive)
func RepoListRecords(ctx context.Context, c *xrpc.Client, collection string, cursor string, limit int64, repo string, reverse bool, rkeyEnd string, rkeyStart string) (*RepoListRecords_Output, error) {
	var out RepoListRecords_Output

	params := map[string]interface{}{
		"collection": collection,
		"cursor":     cursor,
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
