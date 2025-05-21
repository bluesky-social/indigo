package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

type APIClient struct {
	// inner HTTP client
	Client *http.Client

	// host URL prefix: scheme, hostname, and port. This field is required.
	Host string

	// optional auth client "middleware"
	Auth AuthMethod

	// optional HTTP headers which will be included in all requests. Only a single value per key is included; request-level headers will override any client-level defaults.
	Headers http.Header

	// optional authenticated account DID for this client. Does not change client behavior; this is included as a convenience for calling code, logging, etc.
	AccountDID *syntax.DID
}

// Creates a simple APIClient for the provided host. This is appropriate for use with unauthenticated ("public") atproto API endpoints, or to use as a base client to add authentication.
//
// Uses the default stdlib http.Client, and sets a default User-Agent.
func NewAPIClient(host string) *APIClient {
	return &APIClient{
		Client: http.DefaultClient,
		Host:   host,
		Headers: map[string][]string{
			"User-Agent": []string{"indigo-sdk"},
		},
	}
}

// High-level helper for simple JSON "Query" API calls.
//
// Does not work with all API endpoints. For more control, use the Do() method with APIRequest.
func (c *APIClient) Get(ctx context.Context, endpoint syntax.NSID, params map[string]any, out any) error {

	req := NewAPIRequest(http.MethodGet, endpoint, nil)
	req.Headers.Set("Accept", "application/json")

	if params != nil {
		qp, err := ParseParams(params)
		if err != nil {
			return err
		}
		req.QueryParams = qp
	}

	resp, err := c.Do(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if !(resp.StatusCode >= 200 && resp.StatusCode < 300) {
		var eb ErrorBody
		if err := json.NewDecoder(resp.Body).Decode(&eb); err != nil {
			return &APIError{StatusCode: resp.StatusCode}
		}
		return eb.APIError(resp.StatusCode)
	}

	if out == nil {
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("failed decoding JSON response body: %w", err)
	}
	return nil
}

// High-level helper for simple JSON-to-JSON "Procedure" API calls, with no query params.
//
// Does not work with all possible atproto API endpoints. For more control, use the Do() method with APIRequest.
func (c *APIClient) Post(ctx context.Context, endpoint syntax.NSID, body any, out any) error {
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req := NewAPIRequest(http.MethodPost, endpoint, bytes.NewReader(bodyJSON))
	req.Headers.Set("Accept", "application/json")
	req.Headers.Set("Content-Type", "application/json")

	resp, err := c.Do(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if !(resp.StatusCode >= 200 && resp.StatusCode < 300) {
		var eb ErrorBody
		if err := json.NewDecoder(resp.Body).Decode(&eb); err != nil {
			return &APIError{StatusCode: resp.StatusCode}
		}
		return eb.APIError(resp.StatusCode)
	}

	if out == nil {
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("failed decoding JSON response body: %w", err)
	}
	return nil
}

// Full-featured method for atproto API requests.
//
// NOTE: this does not currently parse error response JSON body, thought it might in the future.
func (c *APIClient) Do(ctx context.Context, req *APIRequest) (*http.Response, error) {

	if c.Client == nil {
		c.Client = http.DefaultClient
	}

	httpReq, err := req.HTTPRequest(ctx, c.Host, c.Headers)
	if err != nil {
		return nil, err
	}

	var resp *http.Response
	if c.Auth != nil {
		resp, err = c.Auth.DoWithAuth(c.Client, httpReq)
	} else {
		resp, err = c.Client.Do(httpReq)
	}
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Returns a shallow copy of the APIClient with the provided service ref configured as a proxy header.
//
// To configure service proxying without creating a copy, simply set the "Atproto-Proxy" header.
func (c *APIClient) WithService(ref string) *APIClient {
	hdr := c.Headers.Clone()
	hdr.Set("Atproto-Proxy", ref)
	out := APIClient{
		Client:     c.Client,
		Host:       c.Host,
		Auth:       c.Auth,
		Headers:    hdr,
		AccountDID: c.AccountDID,
	}
	return &out
}

// Configures labeler header (Atproto-Accept-Labelers) with the indicated "redact" level labelers, and regular labelers.
func (c *APIClient) SetLabelers(redact, other []syntax.DID) {
	val := ""
	for _, did := range redact {
		if val != "" {
			val = val + ","
		}
		val = fmt.Sprintf("%s%s;redact", val, did.String())
	}
	for _, did := range other {
		if val != "" {
			val = val + ","
		}
		val = val + did.String()
	}
	c.Headers.Set("Atproto-Accept-Labelers", val)
}
