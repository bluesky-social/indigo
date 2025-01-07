package identity

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
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
				return "", fmt.Errorf("%w: invalid DID in handle DNS record: %w", ErrHandleResolutionFailed, err)
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
			return "", fmt.Errorf("%w: %s", ErrHandleNotFound, handle)
		}
	}
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrHandleResolutionFailed, err)
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
		return "", fmt.Errorf("%w: DNS error: %w", ErrHandleResolutionFailed, err)
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
		return "", fmt.Errorf("%w: DNS resolution failed: %w", ErrHandleResolutionFailed, err)
	}
	return parseTXTResp(res)
}

// variant of ResolveHandleDNS which uses any configured fallback DNS servers
func (d *BaseDirectory) ResolveHandleDNSFallback(ctx context.Context, handle syntax.Handle) (syntax.DID, error) {
	retErr := fmt.Errorf("no fallback servers configured")
	var dnsErr *net.DNSError
	for _, ns := range d.FallbackDNSServers {
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
				retErr = ErrHandleNotFound
				continue
			}
		}
		if err != nil {
			retErr = fmt.Errorf("%w: %w", ErrHandleResolutionFailed, err)
			continue
		}
		ret, err := parseTXTResp(res)
		if err != nil {
			retErr = err
			continue
		}
		return ret, nil
	}
	return "", retErr
}

func (d *BaseDirectory) ResolveHandleWellKnown(ctx context.Context, handle syntax.Handle) (syntax.DID, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("https://%s/.well-known/atproto-did", handle), nil)
	if err != nil {
		return "", fmt.Errorf("constructing HTTP request for handle resolution: %w", err)
	}

	resp, err := d.HTTPClient.Do(req)
	if err != nil {
		// check for NXDOMAIN
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) {
			if dnsErr.IsNotFound {
				return "", fmt.Errorf("%w: DNS NXDOMAIN for HTTP well-known resolution of %s", ErrHandleNotFound, handle)
			}
		}
		return "", fmt.Errorf("%w: HTTP well-known request error: %w", ErrHandleResolutionFailed, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("%w: HTTP 404 for %s", ErrHandleNotFound, handle)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: HTTP well-known status %d for %s", ErrHandleResolutionFailed, resp.StatusCode, handle)
	}

	if resp.ContentLength > 2048 {
		return "", fmt.Errorf("%w: HTTP well-known body too large for %s", ErrHandleResolutionFailed, handle)
	}

	b, err := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if err != nil {
		return "", fmt.Errorf("%w: HTTP well-known body read for %s: %w", ErrHandleResolutionFailed, handle, err)
	}
	line := strings.TrimSpace(string(b))
	outDid, err := syntax.ParseDID(line)
	if err != nil {
		return outDid, fmt.Errorf("%w: invalid DID in HTTP well-known for %s", ErrHandleResolutionFailed, handle)
	}
	return outDid, err
}

func (d *BaseDirectory) ResolveHandle(ctx context.Context, handle syntax.Handle) (syntax.DID, error) {
	// TODO: *could* do resolution in parallel, but expecting that sequential is sufficient to start
	var dnsErr error
	var did syntax.DID

	if handle.IsInvalidHandle() {
		return "", fmt.Errorf("can not resolve handle: %w", ErrInvalidHandle)
	}

	if !handle.AllowedTLD() {
		return "", ErrHandleReservedTLD
	}

	tryDNS := true
	for _, suffix := range d.SkipDNSDomainSuffixes {
		if strings.HasSuffix(handle.String(), suffix) {
			tryDNS = false
			break
		}
	}

	if tryDNS {
		start := time.Now()
		triedAuthoritative := false
		triedFallback := false
		did, dnsErr = d.ResolveHandleDNS(ctx, handle)
		if errors.Is(dnsErr, ErrHandleNotFound) && d.TryAuthoritativeDNS {
			slog.Debug("attempting authoritative handle DNS resolution", "handle", handle)
			triedAuthoritative = true
			// try harder with authoritative lookup
			did, dnsErr = d.ResolveHandleDNSAuthoritative(ctx, handle)
		}
		if errors.Is(dnsErr, ErrHandleNotFound) && len(d.FallbackDNSServers) > 0 {
			slog.Debug("attempting fallback DNS resolution", "handle", handle)
			triedFallback = true
			// try harder with fallback lookup
			did, dnsErr = d.ResolveHandleDNSFallback(ctx, handle)
		}
		elapsed := time.Since(start)
		slog.Debug("resolve handle DNS", "handle", handle, "err", dnsErr, "did", did, "authoritative", triedAuthoritative, "fallback", triedFallback, "duration_ms", elapsed.Milliseconds())
		if nil == dnsErr { // if *not* an error
			return did, nil
		}
	}

	start := time.Now()
	did, httpErr := d.ResolveHandleWellKnown(ctx, handle)
	elapsed := time.Since(start)
	slog.Debug("resolve handle HTTP well-known", "handle", handle, "err", httpErr, "did", did, "duration_ms", elapsed.Milliseconds())
	if nil == httpErr { // if *not* an error
		return did, nil
	}

	// return the most specific/helpful error
	if !errors.Is(dnsErr, ErrHandleNotFound) {
		return "", dnsErr
	}
	if !errors.Is(httpErr, ErrHandleNotFound) {
		return "", httpErr
	}
	return "", dnsErr
}
