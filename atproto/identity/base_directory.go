package identity

import (
	"context"
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
}

var _ Directory = (*BaseDirectory)(nil)

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
		return nil, err
	}
	if declared != h {
		return nil, fmt.Errorf("handle does not match that declared in DID document")
	}
	ident.Handle = declared

	// optimistic caching of public key
	pk, err := ident.PublicKey()
	if nil == err {
		ident.ParsedPublicKey = pk
	}
	return &ident, nil
}

func (d *BaseDirectory) LookupDID(ctx context.Context, did syntax.DID) (*Identity, error) {
	doc, err := d.ResolveDID(ctx, did)
	if err != nil {
		return nil, err
	}
	ident := ParseIdentity(doc)
	declared, err := ident.DeclaredHandle()
	if err != nil {
		return nil, err
	}
	resolvedDID, err := d.ResolveHandle(ctx, declared)
	if err != nil && err != ErrHandleNotFound {
		return nil, err
	} else if ErrHandleNotFound == err || resolvedDID != did {
		ident.Handle = syntax.HandleInvalid
	} else {
		ident.Handle = declared
	}

	// optimistic caching of public key
	pk, err := ident.PublicKey()
	if nil == err {
		ident.ParsedPublicKey = pk
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
	return nil
}
