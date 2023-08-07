package identity

import (
	"fmt"

	"github.com/bluesky-social/indigo/atproto/identifier"
)

// API for doing account lookups by DID or handle, with bi-directional verification handled automatically.
// Some example implementations of this interface would be:
//   - naive direct resolution on every call
//   - API client, which just makes requests to PDS (or other remote service)
//   - simple in-memory caching wrapper layer to reduce network hits
//   - services with backing datastore to do sophisticated caching, TTL, auto-refresh, etc
type AccountCatalog interface {
	LookupHandle(h identifier.Handle) (*Account, error)
	LookupDID(h identifier.DID) (*Account, error)
}

// Represents an atproto identity. Could be a regular user account, or a service account (eg, feed generator)
type Account struct {
	// these fields are required and non-nullable
	Handle identifier.Handle
	DID    identifier.DID

	// these fields are nullable
	PdsServiceUrl       string
	FeedServiceUrl      string
	SigningKeyMultibase string
}

func (a *Account) DidDocument() interface{} {
	panic("NOT IMPLEMENTED")
}

// Does not cross-verify, just does the handle resolution step.
func ResolveHandle(handle identifier.Handle) (identifier.DID, error) {
	// naive implementation tries DNS first then falls back to HTTP well-known
	// sophisticated implementation would do both in parallel, and if the first to finish is successful, cancels the second method
	panic("NOT IMPLEMENTED")
}

// WARNING: this does *not* bi-directionally verify account metadata; it only implements direct DID-to-DID-document lookup for the supported DID methods, and parses the resulting DID Doc into an Account struct
func ResolveDID(did identifier.DID) (*Account, error) {
	switch did.Method() {
	case "web":
		panic("NOT IMPLEMENTED")
	case "plc":
		panic("NOT IMPLEMENTED")
	default:
		return nil, fmt.Errorf("DID method not supported: %s", did.Method())
	}
}
