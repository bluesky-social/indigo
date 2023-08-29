// Code generated by cmd/lexgen (see Makefile's lexgen); DO NOT EDIT.

package atproto

// schema: com.atproto.sync.listBlobs

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// SyncListBlobs_Output is the output of a com.atproto.sync.listBlobs call.
type SyncListBlobs_Output struct {
	Cids []string `json:"cids" cborgen:"cids"`
}

// SyncListBlobs calls the XRPC method "com.atproto.sync.listBlobs".
//
// did: The DID of the repo.
// earliest: The earliest commit to start from
// latest: The most recent commit
func SyncListBlobs(ctx context.Context, c *xrpc.Client, did string, earliest string, latest string) (*SyncListBlobs_Output, error) {
	var out SyncListBlobs_Output

	params := map[string]interface{}{
		"did":      did,
		"earliest": earliest,
		"latest":   latest,
	}
	c.Mux.RLock()
	defer c.Mux.RUnlock()
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.sync.listBlobs", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
