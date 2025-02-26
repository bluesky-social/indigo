package apidir

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

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
	DID        syntax.DID       `json:"did"`
	Handle     *syntax.Handle   `json:"handle,omitempty"`
	DIDDoc     *json.RawMessage `json:"didDoc,omitempty"`
	Tombstoned bool             `json:"tombstoned"`
}

type didBody struct {
	DIDDoc json.RawMessage `json:"didDoc,omitempty"`
}

type handleBody struct {
	DID syntax.DID `json:"did"`
}

type errorBody struct {
	Name    string `json:"error"`
	Message string `json:"message,omitempty"`
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

func (dir *APIDirectory) ResolveDIDRaw(ctx context.Context, did syntax.DID) (json.RawMessage, error) {
	var body didBody
	u := dir.Host + "/xrpc/com.atproto.identity.resolveDid?did=" + did.String()

	if err := dir.apiGet(ctx, u, &body, identity.ErrDIDResolutionFailed, identity.ErrDIDNotFound); err != nil {
		return nil, err
	}

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
	var body handleBody
	u := dir.Host + "/xrpc/com.atproto.identity.resolveHandle?handle=" + handle.String()

	if err := dir.apiGet(ctx, u, &body, identity.ErrHandleResolutionFailed, identity.ErrHandleNotFound); err != nil {
		return "", err
	}

	return body.DID, nil
}

func (dir *APIDirectory) Lookup(ctx context.Context, atid syntax.AtIdentifier) (*identity.Identity, error) {
	var body identityBody
	u := dir.Host + "/xrpc/com.atproto.identity.resolveIdentity?identifier=" + atid.String()

	// TODO: detect atid type, use that for errors? or just assume DID?
	if err := dir.apiGet(ctx, u, &body, identity.ErrDIDResolutionFailed, identity.ErrDIDNotFound); err != nil {
		return nil, err
	}

	if body.Tombstoned || body.DIDDoc == nil {
		return nil, identity.ErrDIDNotFound
	}

	var doc identity.DIDDocument
	if err := json.Unmarshal(*body.DIDDoc, &doc); err != nil {
		return nil, fmt.Errorf("%w: JSON DID document parse: %w", identity.ErrDIDResolutionFailed, err)
	}

	ident := identity.ParseIdentity(&doc)
	if body.Handle != nil {
		ident.Handle = *body.Handle
	} else {
		ident.Handle = syntax.Handle("invalid.handle")
	}

	return &ident, nil
}

func (dir *APIDirectory) LookupHandle(ctx context.Context, handle syntax.Handle) (*identity.Identity, error) {
	return dir.Lookup(ctx, handle.AtIdentifier())
}

func (dir *APIDirectory) LookupDID(ctx context.Context, did syntax.DID) (*identity.Identity, error) {
	return dir.Lookup(ctx, did.AtIdentifier())
}

func (dir *APIDirectory) Purge(ctx context.Context, atid syntax.AtIdentifier) error {
	var body identityBody
	u := dir.Host + "/xrpc/com.atproto.identity.refreshIdentity?identifier=" + atid.String()

	if err := dir.apiGet(ctx, u, &body, identity.ErrDIDResolutionFailed, identity.ErrDIDNotFound); err != nil {
		return err
	}

	return nil
}
