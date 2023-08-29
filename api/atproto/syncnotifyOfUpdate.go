// Code generated by cmd/lexgen (see Makefile's lexgen); DO NOT EDIT.

package atproto

// schema: com.atproto.sync.notifyOfUpdate

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// SyncNotifyOfUpdate_Input is the input argument to a com.atproto.sync.notifyOfUpdate call.
type SyncNotifyOfUpdate_Input struct {
	// hostname: Hostname of the service that is notifying of update.
	Hostname string `json:"hostname" cborgen:"hostname"`
}

// SyncNotifyOfUpdate calls the XRPC method "com.atproto.sync.notifyOfUpdate".
func SyncNotifyOfUpdate(ctx context.Context, c *xrpc.Client, input *SyncNotifyOfUpdate_Input) error {
	c.Mux.RLock()
	defer c.Mux.RUnlock()
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.sync.notifyOfUpdate", nil, input, nil); err != nil {
		return err
	}

	return nil
}
