package identity

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"golang.org/x/time/rate"
)

// The zero value ('BaseDirectory{}') is a usable Directory.
type BaseDirectory struct {
	// if non-empty, this string should have URL method, hostname, and optional port; it should not have a path or trailing slash
	PLCURL string
	// If not nil, this limiter will be used to rate-limit requests to the PLCURL
	PLCLimiter *rate.Limiter
	// If not nil, this function will be called inline with DID Web lookups, and can be used to limit the number of requests to a given hostname
	DIDWebLimitFunc func(ctx context.Context, hostname string) error
	// HTTP client used for did:web, did:plc, and HTTP (well-known) handle resolution
	HTTPClient http.Client
	// DNS resolver used for DNS handle resolution. Calling code can use a custom Dialer to query against a specific DNS server, or re-implement the interface for even more control over the resolution process
	Resolver net.Resolver
	// when doing DNS handle resolution, should this resolver attempt re-try against an authoritative nameserver if the first TXT lookup fails?
	TryAuthoritativeDNS bool
	// set of handle domain suffixes for for which DNS handle resolution will be skipped
	SkipDNSDomainSuffixes []string
	// set of fallback DNS servers (eg, domain registrars) to try as a fallback. each entry should be "ip:port", eg "8.8.8.8:53"
	FallbackDNSServers []string
	// skips bi-directional verification of handles when doing DID lookups (eg, `LookupDID`). Does not impact direct resolution (`ResolveHandle`) or handle-specific lookup (`LookupHandle`).
	//
	// The intended use-case for this flag is as an optimization for services which do not care about handles, but still want to use the `Directory` interface (instead of `ResolveDID`). For example, relay implementations, or services validating inter-service auth requests.
	SkipHandleVerification bool
}

var _ Directory = (*BaseDirectory)(nil)
var _ Resolver = (*BaseDirectory)(nil)

func (d *BaseDirectory) LookupHandle(ctx context.Context, h syntax.Handle) (*Identity, error) {
	h = h.Normalize()
	did, err := d.ResolveHandle(ctx, h)
	if err != nil {
		return nil, err
	}
	doc, err := d.ResolveDID(ctx, did)
	if err != nil {
		return nil, err
	}
	ident := ParseIdentity(doc)
	declared, err := ident.DeclaredHandle()
	if err != nil {
		return nil, fmt.Errorf("could not verify handle/DID match: %w", err)
	}
	if declared != h {
		return nil, fmt.Errorf("%w: %s != %s", ErrHandleMismatch, declared, h)
	}
	ident.Handle = declared

	return &ident, nil
}

func (d *BaseDirectory) LookupDID(ctx context.Context, did syntax.DID) (*Identity, error) {
	doc, err := d.ResolveDID(ctx, did)
	if err != nil {
		return nil, err
	}
	ident := ParseIdentity(doc)
	if d.SkipHandleVerification {
		ident.Handle = syntax.HandleInvalid
		return &ident, nil
	}
	declared, err := ident.DeclaredHandle()
	if errors.Is(err, ErrHandleNotDeclared) {
		ident.Handle = syntax.HandleInvalid
	} else if err != nil {
		return nil, fmt.Errorf("could not parse handle from DID document: %w", err)
	} else {
		// if a handle was declared, resolve it
		resolvedDID, err := d.ResolveHandle(ctx, declared)
		if err != nil {
			if errors.Is(err, ErrHandleNotFound) || errors.Is(err, ErrHandleResolutionFailed) {
				ident.Handle = syntax.HandleInvalid
			} else {
				return nil, err
			}
		} else if resolvedDID != did {
			ident.Handle = syntax.HandleInvalid
		} else {
			ident.Handle = declared
		}
	}

	return &ident, nil
}

func (d *BaseDirectory) Lookup(ctx context.Context, a syntax.AtIdentifier) (*Identity, error) {
	handle, err := a.AsHandle()
	if nil == err { // if *not* an error
		return d.LookupHandle(ctx, handle)
	}
	did, err := a.AsDID()
	if nil == err { // if *not* an error
		return d.LookupDID(ctx, did)
	}
	return nil, fmt.Errorf("at-identifier neither a Handle nor a DID")
}

func (d *BaseDirectory) Purge(ctx context.Context, a syntax.AtIdentifier) error {
	// BaseDirectory itself does not implement caching
	return nil
}
