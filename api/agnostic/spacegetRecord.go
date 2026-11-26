// Copied from indigo:api/atproto/spacegetRecord.go

// Lexicon schema: com.atproto.space.getRecord

package agnostic

import (
	"context"
	"encoding/json"

	lexutil "github.com/bluesky-social/indigo/lex/util"
)

// SpaceGetRecord_Output is the output of a com.atproto.space.getRecord call.
type SpaceGetRecord_Output struct {
	Cid   string           `json:"cid" cborgen:"cid"`
	Uri   string           `json:"uri" cborgen:"uri"`
	Value *json.RawMessage `json:"value" cborgen:"value"`
}

// SpaceGetRecord calls the XRPC method "com.atproto.space.getRecord".
func SpaceGetRecord(ctx context.Context, c lexutil.LexClient, collection string, repo string, rkey string, space string) (*SpaceGetRecord_Output, error) {
	var out SpaceGetRecord_Output

	params := map[string]interface{}{}
	params["collection"] = collection
	params["repo"] = repo
	params["rkey"] = rkey
	params["space"] = space
	if err := c.LexDo(ctx, lexutil.Query, "", "com.atproto.space.getRecord", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
