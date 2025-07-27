// Package remote provides a PDS implementation of the Remote interface
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

	"github.com/bluesky-social/indigo/atproto/data"
	"github.com/bluesky-social/indigo/atproto/repo"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"
)

// PdsRemote implements the Remote interface for reading from CAR files
type PdsRemote struct {
	Service string
}

// ListRepos returns the DIDs hosted by the PDS
func (p *PdsRemote) ListRepos(ctx context.Context, params ListReposParams) (*ListReposResult, error) {
	// Build URL with query parameters
	u, err := url.Parse(fmt.Sprintf("%s/xrpc/com.atproto.sync.listRepos", p.Service))
	if err != nil {
		return nil, fmt.Errorf("failed to parse PDS URL: %w", err)
	}

	q := u.Query()
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

// FetchRepo streams the contents of a repository from the PDS
func (p *PdsRemote) FetchRepo(ctx context.Context, params FetchRepoParams) (*RemoteStream, error) {
	// Clone repo
	url := fmt.Sprintf("%s/xrpc/com.atproto.sync.getRepo?did=%s", p.Service, params.Did)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.ipld.car")
	req.Header.Set("User-Agent", "bsky-butterfly/0.0.1")

	// TODO
	// if s.magicHeaderKey != "" && s.magicHeaderVal != "" {
	// 	req.Header.Set(s.magicHeaderKey, s.magicHeaderVal)
	// }

	client := &http.Client{
		Timeout: 180 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get repo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get repo: %s", resp.Status)
	}

	commit, r, err := repo.LoadRepoFromCAR(ctx, resp.Body)
	if err != nil {
		return nil, fmt.Errorf("Failed to read repo from CAR: %w", err)
	}

	// Create stream with cancellable context
	streamCtx, cancel := context.WithCancel(ctx)
	stream := &RemoteStream{
		Ch:     make(chan StreamEvent, 100), // Buffer for better performance
		cancel: cancel,
	}

	// Stream repository contents
	go func() {
		defer close(stream.Ch)
		defer cancel()

		// Send records from the repository
		err := r.MST.Walk(func(k []byte, v cid.Cid) error {
			// Check for cancellation
			select {
			case <-streamCtx.Done():
				return streamCtx.Err()
			default:
			}

			col, rkey, err := syntax.ParseRepoPath(string(k))
			if err != nil {
				return fmt.Errorf("invalid repo path %q: %w", string(k), err)
			}

			// Skip if collections filter is specified
			if len(params.Collections) > 0 {
				found := false
				for _, c := range params.Collections {
					if c == col.String() {
						found = true
						break
					}
				}
				if !found {
					return nil
				}
			}

			recBytes, _, err := r.GetRecordBytes(streamCtx, col, rkey)
			if err != nil {
				return fmt.Errorf("failed to get record %s/%s: %w", col, rkey, err)
			}

			rec, err := data.UnmarshalCBOR(recBytes)
			if err != nil {
				return fmt.Errorf("failed to unmarshal record %s/%s: %w", col, rkey, err)
			}

			event := StreamEvent{
				Did:       params.Did,
				Timestamp: time.Now(), // TODO - CAR files don't have timestamps?
				Kind:      EventKindCommit,
				Commit: &StreamEventCommit{
					Rev:        commit.Rev, // TODO - is this accurate?
					Operation:  OpCreate,
					Collection: col.String(),
					Rkey:       rkey.String(),
					Record:     rec,
					Cid:        v.String(),
				},
			}

			select {
			case stream.Ch <- event:
			case <-streamCtx.Done():
				return streamCtx.Err()
			}

			return nil
		})

		if err != nil {
			// Send error event
			select {
			case stream.Ch <- StreamEvent{
				Did:       params.Did,
				Timestamp: time.Now(),
				Kind:      EventKindError,
				Error: &StreamEventError{
					Err:   err,
					Fatal: true,
				},
			}:
			case <-streamCtx.Done():
			}
		}
	}()

	return stream, nil
}

// SubscribeRecords subscribes to the record event-stream of the remote
func (p *PdsRemote) SubscribeRecords(ctx context.Context, params SubscribeRecordsParams) (*RemoteStream, error) {
	// TODO
	return nil, fmt.Errorf("subscribe records: %w", ErrNotImplemented)
}
