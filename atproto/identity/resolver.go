package identity

import (
	"context"
	"encoding/json"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// Low-level interface for resolving DIDs and atproto handles.
//
// Most atproto code should use the `identity.Directory` interface instead.
type Resolver interface {
	ResolveDID(ctx context.Context, did syntax.DID) (*DIDDocument, error)
	ResolveDIDRaw(ctx context.Context, did syntax.DID) (json.RawMessage, error)
	ResolveHandle(ctx context.Context, handle syntax.Handle) (syntax.DID, error)
}
