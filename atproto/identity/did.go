package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

type DIDDocument struct {
	DID                syntax.DID              `json:"id"`
	AlsoKnownAs        []string                `json:"alsoKnownAs,omitempty"`
	VerificationMethod []DocVerificationMethod `json:"verificationMethod,omitempty"`
	Service            []DocService            `json:"service,omitempty"`
}

type DocVerificationMethod struct {
	ID                 string `json:"id"`
	Type               string `json:"type"`
	Controller         string `json:"controller"`
	PublicKeyMultibase string `json:"publicKeyMultibase"`
}

type DocService struct {
	ID              string `json:"id"`
	Type            string `json:"type"`
	ServiceEndpoint string `json:"serviceEndpoint"`
}

// WARNING: this does *not* bi-directionally verify account metadata; it only implements direct DID-to-DID-document lookup for the supported DID methods, and parses the resulting DID Doc into an Identity struct
func DefaultResolveDID(ctx context.Context, did syntax.DID) (*DIDDocument, error) {
	switch did.Method() {
	case "web":
		return ResolveDIDWeb(ctx, did)
	case "plc":
		return ResolveDIDPLC(ctx, DefaultPLCURL, did)
	default:
		return nil, fmt.Errorf("DID method not supported: %s", did.Method())
	}
}

func ResolveDIDWeb(ctx context.Context, did syntax.DID) (*DIDDocument, error) {
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

	// TODO: use a more robust client
	// TODO: allow ctx to specify unsafe http:// resolution, for testing?
	resp, err := http.Get("https://" + hostname + "/.well-known/did.json")
	// look for NXDOMAIN
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		if dnsErr.IsNotFound {
			return nil, ErrDIDNotFound
		}
	}
	if err != nil {
		return nil, fmt.Errorf("failed HTTP fetch of did:web well-known document: %w", err)
	}
	if resp.StatusCode == 404 {
		return nil, ErrDIDNotFound
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed did:web well-known fetch, HTTP status: %d", resp.StatusCode)
	}

	var doc DIDDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("failed parse of did:web document JSON: %w", err)
	}
	return &doc, nil
}

// plcURL should have URL method, hostname, and optional port; it should not have a path or trailing slash
func ResolveDIDPLC(ctx context.Context, plcURL string, did syntax.DID) (*DIDDocument, error) {
	if did.Method() != "plc" {
		return nil, fmt.Errorf("expected a did:plc, got: %s", did)
	}

	resp, err := http.Get(plcURL + "/" + did.String())
	if err != nil {
		return nil, fmt.Errorf("failed did:plc directory resolution: %w", err)
	}
	if resp.StatusCode == 404 {
		return nil, ErrDIDNotFound
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed did:web well-known fetch, HTTP status: %d", resp.StatusCode)
	}

	var doc DIDDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("failed parse of did:plc document JSON: %w", err)
	}
	return &doc, nil
}
