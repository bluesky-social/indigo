package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

func XrpcListRepos(ctx context.Context, service string, params ListReposParams) (*ListReposResult, error) {
	var xrpcMethod = "com.atproto.sync.listRepos"
	if params.Collection != "" {
		xrpcMethod = "com.atproto.sync.listReposByCollection"
	}

	// Build URL with query parameters
	u, err := url.Parse(fmt.Sprintf("%s/xrpc/%s", service, xrpcMethod))
	if err != nil {
		return nil, fmt.Errorf("failed to parse PDS URL: %w", err)
	}

	q := u.Query()
	if params.Collection != "" {
		q.Set("collection", params.Collection)
	}
	if params.Cursor != "" {
		q.Set("cursor", params.Cursor)
	}
	if params.Limit > 0 {
		q.Set("limit", strconv.Itoa(params.Limit))
	}
	u.RawQuery = q.Encode()

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "butterfly/0.0.1")

	// Make request
	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to list repos: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to list repos: %s - %s", resp.Status, string(body))
	}

	// Parse response
	var apiResp struct {
		Cursor *string `json:"cursor,omitempty"`
		Repos  []struct {
			Did    string  `json:"did"`
			Head   string  `json:"head"`
			Rev    string  `json:"rev"`
			Active *bool   `json:"active,omitempty"`
			Status *string `json:"status,omitempty"`
		} `json:"repos"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Extract DIDs from response
	dids := make([]string, 0, len(apiResp.Repos))
	for _, repo := range apiResp.Repos {
		// Only include active repos by default
		if repo.Active == nil || *repo.Active {
			dids = append(dids, repo.Did)
		}
	}

	result := &ListReposResult{
		Dids: dids,
	}
	if apiResp.Cursor != nil {
		result.Cursor = *apiResp.Cursor
	}

	return result, nil
}
