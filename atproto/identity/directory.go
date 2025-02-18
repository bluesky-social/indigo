package identity

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// Ergonomic interface for atproto identity lookup, by DID or handle.
//
// The "Lookup" methods resolve identities (handle and DID), and return results in a compact, opinionated struct (`Identity`). They do bi-directional handle/DID verification by default. Clients and services should use these methods by default, instead of resolving handles or DIDs separately.
//
// Looking up a handle which fails to resolve, or don't match DID alsoKnownAs, returns an error. When looking up a DID, if the handle does not resolve back to the DID, the lookup succeeds and returns an `Identity` where the Handle is the special `handle.invalid` value.
//
// Some example implementations of this interface could be:
//   - basic direct resolution on every call
//   - local in-memory caching layer to reduce network hits
//   - API client, which just makes requests to PDS (or other remote service)
//   - client for shared network cache (eg, Redis)
type Directory interface {
	LookupHandle(ctx context.Context, handle syntax.Handle) (*Identity, error)
	LookupDID(ctx context.Context, did syntax.DID) (*Identity, error)
	Lookup(ctx context.Context, atid syntax.AtIdentifier) (*Identity, error)

	// Flushes any cache of the indicated identifier. If directory is not using caching, can ignore this.
	Purge(ctx context.Context, i syntax.AtIdentifier) error
}

// Indicates that handle resolution failed. A wrapped error may provide more context. This is only returned when looking up a handle, not when looking up a DID.
var ErrHandleResolutionFailed = errors.New("handle resolution failed")

// Indicates that resolution process completed successfully, but handle does not exist. This is only returned when looking up a handle, not when looking up a DID.
var ErrHandleNotFound = errors.New("handle not found")

// Indicates that resolution process completed successfully, handle mapped to a different DID. This is only returned when looking up a handle, not when looking up a DID.
var ErrHandleMismatch = errors.New("handle/DID mismatch")

// Indicates that DID document did not include any handle ("alsoKnownAs"). This is only returned when looking up a handle, not when looking up a DID.
var ErrHandleNotDeclared = errors.New("DID document did not declare a handle")

// Handle top-level domain (TLD) is one of the special "Reserved" suffixes, and not allowed for atproto use
var ErrHandleReservedTLD = errors.New("handle top-level domain is disallowed")

// Indicates that resolution process completed successfully, but the DID does not exist.
var ErrDIDNotFound = errors.New("DID not found")

// Indicates that DID resolution process failed. A wrapped error may provide more context.
var ErrDIDResolutionFailed = errors.New("DID resolution failed")

// Indicates that DID document did not include a public key with the specified ID
var ErrKeyNotDeclared = errors.New("DID document did not declare a relevant public key")

// Handle was invalid, in a situation where a valid handle is required.
var ErrInvalidHandle = errors.New("Invalid Handle")

var DefaultPLCURL = "https://plc.directory"

// Returns a reasonable Directory implementation for applications
func DefaultDirectory() Directory {
	base := BaseDirectory{
		PLCURL: DefaultPLCURL,
		HTTPClient: http.Client{
			Timeout: time.Second * 10,
			Transport: &http.Transport{
				// would want this around 100ms for services doing lots of handle resolution. Impacts PLC connections as well, but not too bad.
				IdleConnTimeout: time.Millisecond * 1000,
				MaxIdleConns:    100,
			},
		},
		Resolver: net.Resolver{
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{Timeout: time.Second * 3}
				return d.DialContext(ctx, network, address)
			},
		},
		TryAuthoritativeDNS: true,
		// primary Bluesky PDS instance only supports HTTP resolution method
		SkipDNSDomainSuffixes: []string{".bsky.social"},
	}
	cached := NewCacheDirectory(&base, 250_000, time.Hour*24, time.Minute*2, time.Minute*5)
	return &cached
}
