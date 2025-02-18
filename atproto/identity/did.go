package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// Resolves a DID to a parsed `DIDDocument` struct.
//
// This method does not bi-directionally verify handles. Most atproto-specific code should use the `identity.Directory` interface ("Lookup" methods), which implement that check by default, and provide more ergonomic helpers for working with atproto-relevant information in DID documents.
//
// Note that the `DIDDocument` might not include all the information in the original document. Use `ResolveDIDRaw()` to get the full original JSON.
func (d *BaseDirectory) ResolveDID(ctx context.Context, did syntax.DID) (*DIDDocument, error) {
	b, err := d.resolveDIDBytes(ctx, did)
	if err != nil {
		return nil, err
	}

	var doc DIDDocument
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("%w: JSON DID document parse: %w", ErrDIDResolutionFailed, err)
	}
	if doc.DID != did {
		return nil, fmt.Errorf("document ID did not match DID")
	}
	return &doc, nil
}

// Low-level method for resolving a DID to a raw JSON document.
//
// This method does not parse the DID document into an atproto-specific format, and does not bi-directionally verify handles. Most atproto-specific code should use the "Lookup*" methods, which do implement that functionality.
func (d *BaseDirectory) ResolveDIDRaw(ctx context.Context, did syntax.DID) (json.RawMessage, error) {
	b, err := d.resolveDIDBytes(ctx, did)
	if err != nil {
		return nil, err
	}

	// parse as doc, to validate at least some syntax
	var doc DIDDocument
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("%w: JSON DID document parse: %w", ErrDIDResolutionFailed, err)
	}
	if doc.DID != did {
		return nil, fmt.Errorf("document ID did not match DID")
	}

	return json.RawMessage(b), nil
}

func (d *BaseDirectory) resolveDIDBytes(ctx context.Context, did syntax.DID) ([]byte, error) {
	var b []byte
	var err error
	start := time.Now()
	switch did.Method() {
	case "web":
		b, err = d.resolveDIDWeb(ctx, did)
	case "plc":
		b, err = d.resolveDIDPLC(ctx, did)
	default:
		return nil, fmt.Errorf("DID method not supported: %s", did.Method())
	}
	elapsed := time.Since(start)
	slog.Debug("resolve DID", "did", did, "err", err, "duration_ms", elapsed.Milliseconds())
	return b, err
}

func (d *BaseDirectory) resolveDIDWeb(ctx context.Context, did syntax.DID) ([]byte, error) {
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

	req, err := http.NewRequestWithContext(ctx, "GET", "https://"+hostname+"/.well-known/did.json", nil)
	if err != nil {
		return nil, fmt.Errorf("constructing HTTP request for did:web resolution: %w", err)
	}
	resp, err := d.HTTPClient.Do(req)

	// look for NXDOMAIN
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		if dnsErr.IsNotFound {
			return nil, fmt.Errorf("%w: DNS NXDOMAIN", ErrDIDNotFound)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("%w: did:web HTTP well-known fetch: %w", ErrDIDResolutionFailed, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: did:web HTTP status 404", ErrDIDNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: did:web HTTP status %d", ErrDIDResolutionFailed, resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

func (d *BaseDirectory) resolveDIDPLC(ctx context.Context, did syntax.DID) ([]byte, error) {
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

	req, err := http.NewRequestWithContext(ctx, "GET", plcURL+"/"+did.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("constructing HTTP request for did:plc resolution: %w", err)
	}
	resp, err := d.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: PLC directory lookup: %w", ErrDIDResolutionFailed, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: PLC directory 404", ErrDIDNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: PLC directory status %d", ErrDIDResolutionFailed, resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}
