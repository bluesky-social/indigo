package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

type BaseAPIClient struct {
	HTTPClient     *http.Client
	Host           string
	Auth           AuthMethod
	DefaultHeaders map[string]string
}

func (c *BaseAPIClient) Get(ctx context.Context, endpoint syntax.NSID, params map[string]string) (*json.RawMessage, error) {
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
		return nil, fmt.Errorf("non-successful API request status: %d", resp.StatusCode)
	}

	var ret json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&ret); err != nil {
		return nil, fmt.Errorf("expected JSON response body: %w", err)
	}
	return &ret, nil
}

func (c *BaseAPIClient) Post(ctx context.Context, endpoint syntax.NSID, body any) (*json.RawMessage, error) {
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
		return nil, fmt.Errorf("non-successful API request status: %d", resp.StatusCode)
	}

	var ret json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&ret); err != nil {
		return nil, fmt.Errorf("expected JSON response body: %w", err)
	}
	return &ret, nil
}

func (c *BaseAPIClient) Do(ctx context.Context, req APIRequest) (*http.Response, error) {
	httpReq, err := req.HTTPRequest(ctx, c.Host, c.DefaultHeaders)
	if err != nil {
		return nil, err
	}

	var resp *http.Response
	if c.Auth != nil {
		resp, err = c.Auth.DoWithAuth(ctx, httpReq, c.HTTPClient)
	} else {
		resp, err = c.HTTPClient.Do(httpReq)
	}
	if err != nil {
		return nil, err
	}
	// TODO: handle some common response errors: rate-limits, 5xx, auth required, etc
	return resp, nil
}

func (c *BaseAPIClient) AuthDID() syntax.DID {
	if c.Auth != nil {
		return c.Auth.AccountDID()
	}
	return ""
}
