package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

type APIClient struct {
	// inner HTTP client
	Client         *http.Client

	// host URL prefix: scheme, hostname, and port. This field is required.
	Host           string

	// optional auth client "middleware"
	Auth           AuthMethod

	// optional HTTP headers which will be included in all requests. Only a single value per key is included; request-level headers will override any client-level defaults.
	DefaultHeaders http.Header
}

// High-level helper for simple JSON "Query" API calls.
//
// Does not work with all API endpoints. For more control, use the Do() method with APIRequest.
func (c *APIClient) Get(ctx context.Context, endpoint syntax.NSID, params url.Values, out any) error {
	hdr := map[string][]string{
		"Accept": []string{"application/json"},
	}
	req := APIRequest{
		Method:      http.MethodGet,
		Endpoint:    endpoint,
		Body:        nil,
		QueryParams: params,
		Headers:     hdr,
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
	hdr := map[string][]string{
		"Accept":       []string{"application/json"},
		"Content-Type": []string{"application/json"},
	}
	req := APIRequest{
		Method:      http.MethodPost,
		Endpoint:    endpoint,
		Body:        bytes.NewReader(bodyJSON),
		QueryParams: nil,
		Headers:     hdr,
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

// Full-power method for atproto API requests.
//
// NOTE: this does not currently parse error response JSON body, thought it might in the future.
func (c *APIClient) Do(ctx context.Context, req APIRequest) (*http.Response, error) {
	httpReq, err := req.HTTPRequest(ctx, c.Host, c.DefaultHeaders)
	if err != nil {
		return nil, err
	}

	// NOTE: this updates the client object itself
	if c.Client == nil {
		c.Client = http.DefaultClient
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

func NewPublicClient(host string) *APIClient {
	return &APIClient{
		Client: http.DefaultClient,
		Host: host,
		DefaultHeaders: map[string][]string{
			"User-Agent": []string{"indigo-sdk"},
		},
	}
}
