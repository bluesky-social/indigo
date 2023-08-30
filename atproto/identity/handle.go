package identity

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// Does not cross-verify, just does the handle resolution step.
func (d *BaseDirectory) ResolveHandleDNS(ctx context.Context, handle syntax.Handle) (syntax.DID, error) {
	// TODO: timeout
	// TODO: mechanism to control NDS better; context? separate method?

	res, err := net.LookupTXT("_atproto." + handle.String())
	// look for NXDOMAIN
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		if dnsErr.IsNotFound {
			return "", ErrHandleNotFound
		}
	}
	if err != nil {
		return "", fmt.Errorf("handle DNS resolution failed: %w", err)
	}

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

func (d *BaseDirectory) ResolveHandleWellKnown(ctx context.Context, handle syntax.Handle) (syntax.DID, error) {
	// TODO: timeout
	// TODO: could pull a client or transport from context?
	c := http.DefaultClient

	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("https://%s/.well-known/atproto-did", handle), nil)
	if err != nil {
		return "", err
	}

	resp, err := c.Do(req)
	if err != nil {
		// look for NXDOMAIN
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
	did, dnsErr := d.ResolveHandleDNS(ctx, handle)
	if dnsErr == nil {
		return did, nil
	}
	did, httpErr := d.ResolveHandleWellKnown(ctx, handle)
	if httpErr == nil {
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
