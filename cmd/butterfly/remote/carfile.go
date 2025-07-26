// Package remote provides a CAR file implementation of the Remote interface
package remote

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/bluesky-social/indigo/atproto/data"
	"github.com/bluesky-social/indigo/atproto/repo"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"
)

// CarfileRemote implements the Remote interface for reading from CAR files
type CarfileRemote struct {
	Filepath string
}

// ListRepos returns the DID of the repository in the CAR file
func (c *CarfileRemote) ListRepos(ctx context.Context, params ListReposParams) (*ListReposResult, error) {
	_, _, did, err := c.readCar(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to read CAR file: %w", err)
	}

	return &ListReposResult{
		Dids: []string{did},
	}, nil
}

// FetchRepo streams the contents of a repository from the CAR file
func (c *CarfileRemote) FetchRepo(ctx context.Context, params FetchRepoParams) (*RemoteStream, error) {
	commit, r, did, err := c.readCar(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to read CAR file: %w", err)
	}

	if did != params.Did {
		return nil, fmt.Errorf("%w: %s", ErrRepoNotFound, params.Did)
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
				Did:       did,
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
				Did:       did,
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

// SubscribeRecords is not supported for CAR files
func (c *CarfileRemote) SubscribeRecords(ctx context.Context, params SubscribeRecordsParams) (*RemoteStream, error) {
	return nil, fmt.Errorf("subscribe records: %w", ErrNotSupported)
}

// readCar reads and validates a CAR file
func (c *CarfileRemote) readCar(ctx context.Context) (*repo.Commit, *repo.Repo, string, error) {
	file, err := os.Open(c.Filepath)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	commit, r, err := repo.LoadRepoFromCAR(ctx, file)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to load repo from CAR: %w", err)
	}

	did, err := syntax.ParseDID(commit.DID)
	if err != nil {
		return nil, nil, "", fmt.Errorf("invalid DID in commit: %w", err)
	}

	return commit, r, did.String(), nil
}
