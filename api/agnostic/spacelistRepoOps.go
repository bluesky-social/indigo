// Copied from indigo:api/atproto/spacelistRepoOps.go

// Lexicon schema: com.atproto.space.listRepoOps

package agnostic

import (
	"context"
	"encoding/json"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	lexutil "github.com/bluesky-social/indigo/lex/util"
)

// SpaceListRepoOps_OpEntry is a "opEntry" in the com.atproto.space.listRepoOps schema.
//
// Operations that share a rev were applied in the same commit.
type SpaceListRepoOps_OpEntry struct {
	// cid: CID of the record after the operation. Null for deletes.
	Cid        *string `json:"cid" cborgen:"cid"`
	Collection string  `json:"collection" cborgen:"collection"`
	// prev: CID of the record before the operation. Null for creates.
	Prev *string `json:"prev" cborgen:"prev"`
	Rev  string  `json:"rev" cborgen:"rev"`
	Rkey string  `json:"rkey" cborgen:"rkey"`
	// value: Omitted for deletes, if excludeValues is set, or if the record has since changed.
	Value *json.RawMessage `json:"value,omitempty" cborgen:"value,omitempty"`
}

// SpaceListRepoOps_Output is the output of a com.atproto.space.listRepoOps call.
type SpaceListRepoOps_Output struct {
	// commit: The repo's current commit. A syncer compares it against its own set hash once it has applied the last page, which has no cursor.
	Commit *comatproto.SpaceDefs_Commit `json:"commit" cborgen:"commit"`
	Cursor *string                      `json:"cursor,omitempty" cborgen:"cursor,omitempty"`
	Ops    []*SpaceListRepoOps_OpEntry  `json:"ops" cborgen:"ops"`
}

// SpaceListRepoOps calls the XRPC method "com.atproto.space.listRepoOps".
func SpaceListRepoOps(ctx context.Context, c lexutil.LexClient, cursor string, excludeValues bool, limit int64, repo string, since string, space string) (*SpaceListRepoOps_Output, error) {
	var out SpaceListRepoOps_Output

	params := map[string]interface{}{}
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
	if since != "" {
		params["since"] = since
	}
	params["space"] = space
	if err := c.LexDo(ctx, lexutil.Query, "", "com.atproto.space.listRepoOps", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
