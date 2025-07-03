package apidir

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gander-social/gander-indigo-sovereign/atproto/identity"
	"github.com/gander-social/gander-indigo-sovereign/atproto/syntax"

	"github.com/carlmjohnson/versioninfo"
)

// Does HTTP requests to an identity server, using standard Lexicon endpoints
type APIDirectory struct {
	Client *http.Client
	// API service to make queries to. Includes schema, hostname, and port, but no path or trailing slash. Eg: "http://localhost:6600"
	Host      string
	UserAgent string
}

var _ identity.Directory = (*APIDirectory)(nil)
var _ identity.Resolver = (*APIDirectory)(nil)

type identityBody struct {
	DID    syntax.DID      `json:"did"`
	Handle syntax.Handle   `json:"handle"`
	DIDDoc json.RawMessage `json:"didDoc"`
}

type didBody struct {
	DIDDoc json.RawMessage `json:"didDoc,omitempty"`
}

type handleBody struct {
	DID syntax.DID `json:"did"`
}

func NewAPIDirectory(host string) APIDirectory {
	return APIDirectory{
		Client: &http.Client{
			Timeout: time.Second * 10,
			Transport: &http.Transport{
				IdleConnTimeout: time.Millisecond * 100,
				MaxIdleConns:    100,
			},
		},
		Host:      host,
		UserAgent: "indigo-apidir/" + versioninfo.Short(),
	}
}

// body: struct pointer which can be `json.Unmarshal()`
func (dir *APIDirectory) apiGet(ctx context.Context, u string, body any, errFail error, errNotFound error) error {

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return fmt.Errorf("constructing HTTP request: %w", err)
	}
	if dir.UserAgent != "" {
		req.Header.Set("User-Agent", dir.UserAgent)
	}
	resp, err := dir.Client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: identity service HTTP: %w", errFail, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%w: identity service HTTP: %w", errFail, err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return errNotFound
	}
	if resp.StatusCode != http.StatusOK {
		// TODO: parse error body, handle more error conditions
		return fmt.Errorf("%w: identity service HTTP: %d", errFail, resp.StatusCode)
	}

	if err := json.Unmarshal(b, body); err != nil {
		return fmt.Errorf("%w: identity service HTTP: %w", errFail, err)
	}
	return nil
}

// body: struct pointer which can be `json.Unmarshal()`
func (dir *APIDirectory) apiPost(ctx context.Context, u string, reqBody []byte, body any, errFail error, errNotFound error) error {
	req, err := http.NewRequestWithContext(ctx, "POST", u, bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("constructing HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if dir.UserAgent != "" {
		req.Header.Set("User-Agent", dir.UserAgent)
	}
	resp, err := dir.Client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: identity service HTTP: %w", errFail, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%w: identity service HTTP: %w", errFail, err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return errNotFound
	}
	if resp.StatusCode != http.StatusOK {
		// TODO: parse error body, handle more error conditions
		return fmt.Errorf("%w: identity service HTTP: %d", errFail, resp.StatusCode)
	}

	if err := json.Unmarshal(b, body); err != nil {
		return fmt.Errorf("%w: identity service HTTP: %w", errFail, err)
	}
	return nil
}

func (dir *APIDirectory) ResolveDIDRaw(ctx context.Context, did syntax.DID) (json.RawMessage, error) {
	var body didBody
	u := dir.Host + "/xrpc/com.atproto.identity.resolveDid?did=" + did.String()

	start := time.Now()
	err := dir.apiGet(ctx, u, &body, identity.ErrDIDResolutionFailed, identity.ErrDIDNotFound)
	if err != nil {
		didResolution.WithLabelValues("apidir", "error").Inc()
		didResolutionDuration.WithLabelValues("apidir", "error").Observe(time.Since(start).Seconds())
		return nil, err
	}
	didResolution.WithLabelValues("apidir", "success").Inc()
	didResolutionDuration.WithLabelValues("apidir", "success").Observe(time.Since(start).Seconds())

	return body.DIDDoc, nil
}

func (dir *APIDirectory) ResolveDID(ctx context.Context, did syntax.DID) (*identity.DIDDocument, error) {
	raw, err := dir.ResolveDIDRaw(ctx, did)
	if err != nil {
		return nil, err
	}

	var doc identity.DIDDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("%w: JSON DID document parse: %w", identity.ErrDIDResolutionFailed, err)
	}
	return &doc, nil
}

func (dir *APIDirectory) ResolveHandle(ctx context.Context, handle syntax.Handle) (syntax.DID, error) {
	handle = handle.Normalize()
	var body handleBody
	u := dir.Host + "/xrpc/com.atproto.identity.resolveHandle?handle=" + handle.String()

	start := time.Now()
	err := dir.apiGet(ctx, u, &body, identity.ErrHandleResolutionFailed, identity.ErrHandleNotFound)
	if err != nil {
		handleResolution.WithLabelValues("apidir", "error").Inc()
		handleResolutionDuration.WithLabelValues("apidir", "error").Observe(time.Since(start).Seconds())
		return "", err
	}
	handleResolution.WithLabelValues("apidir", "success").Inc()
	handleResolutionDuration.WithLabelValues("apidir", "success").Observe(time.Since(start).Seconds())

	return body.DID, nil
}

func (dir *APIDirectory) Lookup(ctx context.Context, atid syntax.AtIdentifier) (*identity.Identity, error) {
	var body identityBody
	u := dir.Host + "/xrpc/com.atproto.identity.resolveIdentity?identifier=" + atid.String()

	// TODO: detect atid type, use that for errors? or just assume DID?
	start := time.Now()
	err := dir.apiGet(ctx, u, &body, identity.ErrDIDResolutionFailed, identity.ErrDIDNotFound)
	if err != nil {
		identityResolution.WithLabelValues("apidir", "error").Inc()
		identityResolutionDuration.WithLabelValues("apidir", "error").Observe(time.Since(start).Seconds())
		return nil, err
	}
	identityResolution.WithLabelValues("apidir", "success").Inc()
	identityResolutionDuration.WithLabelValues("apidir", "success").Observe(time.Since(start).Seconds())

	var doc identity.DIDDocument
	if err := json.Unmarshal(body.DIDDoc, &doc); err != nil {
		return nil, fmt.Errorf("%w: JSON DID document parse: %w", identity.ErrDIDResolutionFailed, err)
	}

	ident := identity.ParseIdentity(&doc)
	ident.Handle = body.Handle

	return &ident, nil
}

func (dir *APIDirectory) LookupHandle(ctx context.Context, handle syntax.Handle) (*identity.Identity, error) {
	return dir.Lookup(ctx, handle.AtIdentifier())
}

func (dir *APIDirectory) LookupDID(ctx context.Context, did syntax.DID) (*identity.Identity, error) {
	return dir.Lookup(ctx, did.AtIdentifier())
}

func (dir *APIDirectory) Purge(ctx context.Context, atid syntax.AtIdentifier) error {

	input := map[string]string{
		"identifier": atid.String(),
	}
	reqBody, err := json.Marshal(input)
	if err != nil {
		return err
	}

	var body identityBody
	u := dir.Host + "/xrpc/com.atproto.identity.refreshIdentity"

	if err := dir.apiPost(ctx, u, reqBody, &body, identity.ErrDIDResolutionFailed, identity.ErrDIDNotFound); err != nil {
		return err
	}

	return nil
}
