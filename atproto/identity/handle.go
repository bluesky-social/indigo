package identity

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

func parseTXTResp(res []string) (syntax.DID, error) {
	for _, s := range res {
		if strings.HasPrefix(s, "did=") {
			parts := strings.SplitN(s, "=", 2)
			did, err := syntax.ParseDID(parts[1])
			if err != nil {
				return "", fmt.Errorf("invalid DID in handle DNS record: %w", err)
			}
			return did, nil
		}
	}
	return "", ErrHandleNotFound
}

// Does not cross-verify, only does the handle resolution step.
func (d *BaseDirectory) ResolveHandleDNS(ctx context.Context, handle syntax.Handle) (syntax.DID, error) {
	res, err := d.Resolver.LookupTXT(ctx, "_atproto."+handle.String())
	// check for NXDOMAIN
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		if dnsErr.IsNotFound {
			return "", ErrHandleNotFound
		}
	}
	if err != nil {
		return "", fmt.Errorf("handle DNS resolution failed: %w", err)
	}
	return parseTXTResp(res)
}

// this is a variant of ResolveHandleDNS which first does an authoritative nameserver lookup, then queries there
func (d *BaseDirectory) ResolveHandleDNSAuthoritative(ctx context.Context, handle syntax.Handle) (syntax.DID, error) {
	// lookup nameserver using configured resolver
	resNS, err := d.Resolver.LookupNS(ctx, handle.String())
	// check for NXDOMAIN
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		if dnsErr.IsNotFound {
			return "", ErrHandleNotFound
		}
	}
	if err != nil {
		return "", fmt.Errorf("handle DNS resolution failed: %w", err)
	}
	if len(resNS) == 0 {
		return "", ErrHandleNotFound
	}
	ns := resNS[0].Host
	if !strings.Contains(ns, ":") {
		ns = ns + ":53"
	}

	// create a custom resolver to use the specific nameserver for TXT lookup
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			rd := net.Dialer{
				Timeout: time.Second * 5,
			}
			return rd.DialContext(ctx, network, ns)
		},
	}
	res, err := resolver.LookupTXT(ctx, "_atproto."+handle.String())
	// check for NXDOMAIN
	if errors.As(err, &dnsErr) {
		if dnsErr.IsNotFound {
			return "", ErrHandleNotFound
		}
	}
	if err != nil {
		return "", fmt.Errorf("handle DNS resolution failed: %w", err)
	}
	return parseTXTResp(res)
}

func (d *BaseDirectory) ResolveHandleWellKnown(ctx context.Context, handle syntax.Handle) (syntax.DID, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("https://%s/.well-known/atproto-did", handle), nil)
	if err != nil {
		return "", err
	}

	resp, err := d.HTTPClient.Do(req)
	if err != nil {
		// check for NXDOMAIN
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) {
			if dnsErr.IsNotFound {
				return "", ErrHandleNotFound
			}
		}
		return "", fmt.Errorf("failed to resolve handle (%s) through HTTP well-known route: %s", handle, err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to resolve handle (%s) through HTTP well-known route: status=%d", handle, resp.StatusCode)
	}

	if resp.ContentLength > 2048 {
		return "", fmt.Errorf("HTTP well-known route returned too much data during handle resolution")
	}

	b, err := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if err != nil {
		return "", fmt.Errorf("HTTP well-known response fail to read: %w", err)
	}
	line := strings.TrimSpace(string(b))
	return syntax.ParseDID(line)
}

func (d *BaseDirectory) ResolveHandle(ctx context.Context, handle syntax.Handle) (syntax.DID, error) {
	// TODO: *could* do resolution in parallel, but expecting that sequential is sufficient to start
	did, dnsErr := d.ResolveHandleDNS(ctx, handle)
	if dnsErr == ErrHandleNotFound && d.TryAuthoritativeDNS {
		// try harder with authoritative lookup
		did, dnsErr = d.ResolveHandleDNSAuthoritative(ctx, handle)
	}
	if nil == dnsErr { // if *not* an error
		return did, nil
	}
	did, httpErr := d.ResolveHandleWellKnown(ctx, handle)
	if nil == httpErr { // if *not* an error
		return did, nil
	}

	// return the most specific/helpful error
	if dnsErr != ErrHandleNotFound {
		return "", dnsErr
	}
	if httpErr != ErrHandleNotFound {
		return "", httpErr
	}
	return "", dnsErr
}
