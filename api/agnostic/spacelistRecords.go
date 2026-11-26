// Copied from indigo:api/atproto/spacelistRecord.go

// Lexicon schema: com.atproto.space.listRecords

package agnostic

import (
	"context"
	"encoding/json"

	lexutil "github.com/bluesky-social/indigo/lex/util"
)

// SpaceListRecords_Output is the output of a com.atproto.space.listRecords call.
type SpaceListRecords_Output struct {
	Cursor  *string                    `json:"cursor,omitempty" cborgen:"cursor,omitempty"`
	Records []*SpaceListRecords_Record `json:"records" cborgen:"records"`
}

// SpaceListRecords_Record is a "record" in the com.atproto.space.listRecords schema.
type SpaceListRecords_Record struct {
	Cid        string           `json:"cid" cborgen:"cid"`
	Collection string           `json:"collection" cborgen:"collection"`
	Rkey       string           `json:"rkey" cborgen:"rkey"`
	Value      *json.RawMessage `json:"value,omitempty" cborgen:"value,omitempty"`
}

// SpaceListRecords calls the XRPC method "com.atproto.space.listRecords".
//
// collection: If omitted, lists records across all collections.
func SpaceListRecords(ctx context.Context, c lexutil.LexClient, collection string, cursor string, excludeValues bool, limit int64, repo string, space string) (*SpaceListRecords_Output, error) {
	var out SpaceListRecords_Output

	params := map[string]interface{}{}
	if collection != "" {
		params["collection"] = collection
	}
	if cursor != "" {
		params["cursor"] = cursor
	}
	if excludeValues {
		params["excludeValues"] = excludeValues
	}
	if limit != 0 {
		params["limit"] = limit
	}
	params["repo"] = repo
	params["space"] = space
	if err := c.LexDo(ctx, lexutil.Query, "", "com.atproto.space.listRecords", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
