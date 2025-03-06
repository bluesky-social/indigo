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
	HTTPClient     *http.Client
	Host           string
	Auth           AuthMethod
	DefaultHeaders map[string]string
}

// High-level helper for simple JSON "Query" API calls.
//
// Does not work with all API endpoints. For more control, use the Do() method with APIRequest.
func (c *APIClient) Get(ctx context.Context, endpoint syntax.NSID, params map[string]string) (*json.RawMessage, error) {
	hdr := map[string]string{
		"Accept": "application/json",
	}
	req := APIRequest{
		HTTPVerb:    "GET",
		Endpoint:    endpoint,
		Body:        nil,
		QueryParams: params,
		Headers:     hdr,
	}
	resp, err := c.Do(ctx, req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	// TODO: duplicate error handling with Post()?
	if !(resp.StatusCode >= 200 && resp.StatusCode < 300) {
		var eb ErrorBody
		if err := json.NewDecoder(resp.Body).Decode(&eb); err != nil {
			return nil, &APIError{StatusCode: resp.StatusCode}
		}
		return nil, eb.APIError(resp.StatusCode)
	}

	var ret json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&ret); err != nil {
		return nil, fmt.Errorf("expected JSON response body: %w", err)
	}
	return &ret, nil
}

// High-level helper for simple JSON-to-JSON "Procedure" API calls.
//
// Does not work with all API endpoints. For more control, use the Do() method with APIRequest.
func (c *APIClient) Post(ctx context.Context, endpoint syntax.NSID, body any) (*json.RawMessage, error) {
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	hdr := map[string]string{
		"Accept":       "application/json",
		"Content-Type": "application/json",
	}
	req := APIRequest{
		HTTPVerb:    "POST",
		Endpoint:    endpoint,
		Body:        bytes.NewReader(bodyJSON),
		QueryParams: nil,
		Headers:     hdr,
	}
	resp, err := c.Do(ctx, req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	// TODO: duplicate error handling with Get()?
	if !(resp.StatusCode >= 200 && resp.StatusCode < 300) {
		var eb ErrorBody
		if err := json.NewDecoder(resp.Body).Decode(&eb); err != nil {
			return nil, &APIError{StatusCode: resp.StatusCode}
		}
		return nil, eb.APIError(resp.StatusCode)
	}

	var ret json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&ret); err != nil {
		return nil, fmt.Errorf("expected JSON response body: %w", err)
	}
	return &ret, nil
}

// Full-power method for atproto API requests.
func (c *APIClient) Do(ctx context.Context, req APIRequest) (*http.Response, error) {
	httpReq, err := req.HTTPRequest(ctx, c.Host, c.DefaultHeaders)
	if err != nil {
		return nil, err
	}

	// TODO: thread-safe?
	if c.HTTPClient == nil {
		c.HTTPClient = http.DefaultClient
	}

	var resp *http.Response
	if c.Auth != nil {
		resp, err = c.Auth.DoWithAuth(httpReq, c.HTTPClient)
	} else {
		resp, err = c.HTTPClient.Do(httpReq)
	}
	if err != nil {
		return nil, err
	}
	// TODO: handle some common response errors: rate-limits, 5xx, auth required, etc
	return resp, nil
}
