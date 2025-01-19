package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// WARNING: this does *not* bi-directionally verify account metadata; it only implements direct DID-to-DID-document lookup for the supported DID methods, and parses the resulting DID Doc into an Identity struct
func (d *BaseDirectory) ResolveDID(ctx context.Context, did syntax.DID) (*DIDDocument, error) {
	start := time.Now()
	switch did.Method() {
	case "web":
		doc, err := d.ResolveDIDWeb(ctx, did)
		elapsed := time.Since(start)
		slog.Debug("resolve DID", "did", did, "err", err, "duration_ms", elapsed.Milliseconds())
		return doc, err
	case "plc":
		doc, err := d.ResolveDIDPLC(ctx, did)
		elapsed := time.Since(start)
		slog.Debug("resolve DID", "did", did, "err", err, "duration_ms", elapsed.Milliseconds())
		return doc, err
	default:
		return nil, fmt.Errorf("DID method not supported: %s", did.Method())
	}
}

func (d *BaseDirectory) ResolveDIDWeb(ctx context.Context, did syntax.DID) (*DIDDocument, error) {
	if did.Method() != "web" {
		return nil, fmt.Errorf("expected a did:web, got: %s", did)
	}
	hostname := did.Identifier()
	handle, err := syntax.ParseHandle(hostname)
	if err != nil {
		return nil, fmt.Errorf("did:web identifier not a simple hostname: %s", hostname)
	}
	if !handle.AllowedTLD() {
		return nil, fmt.Errorf("did:web hostname has disallowed TLD: %s", hostname)
	}

	// TODO: allow ctx to specify unsafe http:// resolution, for testing?

	if d.DIDWebLimitFunc != nil {
		if err := d.DIDWebLimitFunc(ctx, hostname); err != nil {
			return nil, fmt.Errorf("did:web limit func returned an error for (%s): %w", hostname, err)
		}
	}

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, "GET", "https://"+hostname+"/.well-known/did.json", nil)
	if err != nil {
		return nil, fmt.Errorf("constructing HTTP request for did:web resolution: %w", err)
	}
	resp, err := d.HTTPClient.Do(req)

	// look for NXDOMAIN
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		if dnsErr.IsNotFound {
			lookupDuration.WithLabelValues("did_web_ident", "error").Observe(time.Since(start).Seconds())
			return nil, fmt.Errorf("%w: DNS NXDOMAIN", ErrDIDNotFound)
		}
	}
	if err != nil {
		lookupDuration.WithLabelValues("did_web_ident", "error").Observe(time.Since(start).Seconds())
		return nil, fmt.Errorf("%w: did:web HTTP well-known fetch: %w", ErrDIDResolutionFailed, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		lookupDuration.WithLabelValues("did_web_ident", "error").Observe(time.Since(start).Seconds())
		return nil, fmt.Errorf("%w: did:web HTTP status 404", ErrDIDNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: did:web HTTP status %d", ErrDIDResolutionFailed, resp.StatusCode)
	}

	var doc DIDDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		lookupDuration.WithLabelValues("did_web_ident", "error").Observe(time.Since(start).Seconds())
		return nil, fmt.Errorf("%w: JSON DID document parse: %w", ErrDIDResolutionFailed, err)
	}
	lookupDuration.WithLabelValues("did_web_ident", "success").Observe(time.Since(start).Seconds())
	return &doc, nil
}

func (d *BaseDirectory) ResolveDIDPLC(ctx context.Context, did syntax.DID) (*DIDDocument, error) {
	if did.Method() != "plc" {
		return nil, fmt.Errorf("expected a did:plc, got: %s", did)
	}

	plcURL := d.PLCURL
	if plcURL == "" {
		plcURL = DefaultPLCURL
	}

	if d.PLCLimiter != nil {
		if err := d.PLCLimiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("failed to wait for PLC limiter: %w", err)
		}
	}

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, "GET", plcURL+"/"+did.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("constructing HTTP request for did:plc resolution: %w", err)
	}
	resp, err := d.HTTPClient.Do(req)
	if err != nil {
		lookupDuration.WithLabelValues("did_plc_ident", "error").Observe(time.Since(start).Seconds())
		return nil, fmt.Errorf("%w: PLC directory lookup: %w", ErrDIDResolutionFailed, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		lookupDuration.WithLabelValues("did_plc_ident", "error").Observe(time.Since(start).Seconds())
		return nil, fmt.Errorf("%w: PLC directory 404", ErrDIDNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		lookupDuration.WithLabelValues("did_plc_ident", "error").Observe(time.Since(start).Seconds())
		return nil, fmt.Errorf("%w: PLC directory status %d", ErrDIDResolutionFailed, resp.StatusCode)
	}

	var doc DIDDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		lookupDuration.WithLabelValues("did_plc_ident", "error").Observe(time.Since(start).Seconds())
		return nil, fmt.Errorf("%w: JSON DID document parse: %w", ErrDIDResolutionFailed, err)
	}

	lookupDuration.WithLabelValues("did_plc_ident", "success").Observe(time.Since(start).Seconds())
	return &doc, nil
}
