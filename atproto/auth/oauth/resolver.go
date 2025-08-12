package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/bluesky-social/indigo/util/ssrf"
)

// Helper for resolving OAuth documents from the public web: client metadata, auth server metadata, etc.
//
// NOTE: configurable caching will likely be added in the future, but is not implemented yet. This struct may become an interface to support more flexible caching and resolution policies.
type Resolver struct {
	Client    *http.Client
	UserAgent string
}

func NewResolver() *Resolver {
	c := http.Client{
		Timeout:   10 * time.Second,
		Transport: ssrf.PublicOnlyTransport(),
	}
	return &Resolver{
		Client:    &c,
		UserAgent: "indigo-sdk",
	}
}

// Resolves a Resource Server URL (eg, an atproto account's registered PDS service URL) to an auth server URL (eg, entryway URL). They might be the same server!
//
// Ensures that the returned URL is valid (eg, parses as a URL).
func (r *Resolver) ResolveAuthServerURL(ctx context.Context, hostURL string) (string, error) {
	u, err := url.Parse(hostURL)
	if err != nil {
		return "", err
	}
	// TODO: check against other resource server rules?
	if u.Scheme != "https" || u.Hostname() == "" || u.Port() != "" {
		return "", fmt.Errorf("not a valid public host URL: %s", hostURL)
	}

	docURL := fmt.Sprintf("https://%s/.well-known/oauth-protected-resource", u.Hostname())

	// NOTE: this allows redirects
	req, err := http.NewRequestWithContext(ctx, "GET", docURL, nil)
	if err != nil {
		return "", err
	}
	if r.UserAgent != "" {
		req.Header.Set("User-Agent", r.UserAgent)
	}

	resp, err := r.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching protected resource document: %w", err)
	}
	defer resp.Body.Close()

	// intentionally check for exactly HTTP 200 (not just 2xx)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP error fetching protected resource document: %d", resp.StatusCode)
	}

	var body ProtectedResourceMetadata
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("invalid protected resource document: %w", err)
	}
	if len(body.AuthorizationServers) < 1 {
		return "", fmt.Errorf("no auth server URL in protected resource document")
	}
	authURL := body.AuthorizationServers[0]
	au, err := url.Parse(body.AuthorizationServers[0])
	if err != nil {
		return "", fmt.Errorf("invalid auth server URL: %w", err)
	}
	if au.Scheme != "https" || au.Hostname() == "" || au.Port() != "" {
		return "", fmt.Errorf("not a valid public auth server URL: %s", authURL)
	}
	return authURL, nil
}

// Resolves an Auth Server URL to server metadata. Validates metadata before returning.
func (r *Resolver) ResolveAuthServerMetadata(ctx context.Context, serverURL string) (*AuthServerMetadata, error) {
	u, err := url.Parse(serverURL)
	if err != nil {
		return nil, err
	}
	// TODO: check against other rules?
	if u.Scheme != "https" || u.Hostname() == "" || u.Port() != "" {
		return nil, fmt.Errorf("not a valid public host URL: %s", serverURL)
	}

	docURL := fmt.Sprintf("https://%s/.well-known/oauth-authorization-server", u.Hostname())

	// NOTE: this allows redirects
	req, err := http.NewRequestWithContext(ctx, "GET", docURL, nil)
	if err != nil {
		return nil, err
	}
	if r.UserAgent != "" {
		req.Header.Set("User-Agent", r.UserAgent)
	}

	resp, err := r.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching auth server metadata: %w", err)
	}
	defer resp.Body.Close()

	// NOTE: maybe any HTTP 2xx should be allowed?
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error fetching auth server metadata: %d", resp.StatusCode)
	}

	var body AuthServerMetadata
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("invalid protected resource document: %w", err)
	}

	if err := body.Validate(serverURL); err != nil {
		return nil, err
	}
	return &body, nil
}

// Fetches and validates OAuth client metadata document based on identifier in URL format.
func (r *Resolver) ResolveClientMetadata(ctx context.Context, clientID string) (*ClientMetadata, error) {
	u, err := url.Parse(clientID)
	if err != nil {
		return nil, err
	}
	// TODO: check against other rules?
	if u.Scheme != "https" || u.Hostname() == "" || u.Port() != "" {
		return nil, fmt.Errorf("not a valid public host URL: %s", clientID)
	}

	// NOTE: this allows redirects
	req, err := http.NewRequestWithContext(ctx, "GET", clientID, nil)
	if err != nil {
		return nil, err
	}
	if r.UserAgent != "" {
		req.Header.Set("User-Agent", r.UserAgent)
	}

	resp, err := r.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching client metadata: %w", err)
	}
	defer resp.Body.Close()

	// NOTE: maybe any HTTP 2xx should be allowed?
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error fetching auth server metadata: %d", resp.StatusCode)
	}

	var body ClientMetadata
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("invalid client metadata document: %w", err)
	}

	if err := body.Validate(clientID); err != nil {
		return nil, err
	}
	return &body, nil
}
