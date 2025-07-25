// Package remote defines interfaces for fetching AT Protocol data from various sources
package remote

import (
	"context"
	"errors"
	"time"
)

// Remote defines the interface for data sources in the butterfly sync engine
type Remote interface {
	// ListRepos lists repositories hosted at the given remote
	// Not all remotes will support all parameters
	ListRepos(ctx context.Context, params ListReposParams) (*ListReposResult, error)

	// FetchRepo fetches the contents of the requested repository
	// Not all remotes will support all parameters
	FetchRepo(ctx context.Context, params FetchRepoParams) (*RemoteStream, error)

	// SubscribeRecords subscribes to the record event-stream of the remote
	// Not all remotes will support all parameters
	SubscribeRecords(ctx context.Context, params SubscribeRecordsParams) (*RemoteStream, error)
}

// ListReposParams contains parameters for listing repositories
type ListReposParams struct {
	Collection string
	Cursor     string
	Limit      int
}

// ListReposResult contains the result of a repository listing
type ListReposResult struct {
	Dids   []string
	Cursor string // For pagination
}

// FetchRepoParams contains parameters for fetching a repository
type FetchRepoParams struct {
	Did         string
	Collections []string
	Since       *string // Optional: fetch only changes since this revision
}

// SubscribeRecordsParams contains parameters for subscribing to records
type SubscribeRecordsParams struct {
	Dids        []string
	Collections []string
	Cursor      int64 // Resume from this cursor position
}

// RemoteStream represents a stream of events from a remote
type RemoteStream struct {
	Ch     chan StreamEvent
	cancel context.CancelFunc
}

// Close closes the stream
func (s *RemoteStream) Close() error {
	if s.cancel != nil {
		s.cancel()
	}
	return nil
}

// StreamEventKind represents the type of stream event
type StreamEventKind string

const (
	EventKindCommit   StreamEventKind = "commit"
	EventKindIdentity StreamEventKind = "identity"
	EventKindAccount  StreamEventKind = "account"
	EventKindError    StreamEventKind = "error"
)

// StreamEvent represents an event from the remote stream
type StreamEvent struct {
	Did       string
	Timestamp time.Time
	Kind      StreamEventKind

	// Event-specific data (only one will be populated based on Kind)
	Commit   *StreamEventCommit
	Identity *StreamEventIdentity
	Account  *StreamEventAccount
	Error    *StreamEventError
}

// CommitOperation represents the type of commit operation
type CommitOperation string

const (
	OpCreate CommitOperation = "create"
	OpUpdate CommitOperation = "update"
	OpDelete CommitOperation = "delete"
)

// StreamEventCommit represents a repository commit event
type StreamEventCommit struct {
	Rev        string
	Operation  CommitOperation
	Collection string
	Rkey       string
	Record     map[string]any
	Cid        string
}

// StreamEventIdentity represents an identity update event
type StreamEventIdentity struct {
	Did    string
	Handle string
	Seq    uint64
	Time   time.Time
}

// StreamEventAccount represents an account status change event
type StreamEventAccount struct {
	Active bool
	Did    string
	Seq    uint64
	Time   time.Time
}

// StreamEventError represents an error event in the stream
type StreamEventError struct {
	Err       error
	Fatal     bool   // Whether this error terminates the stream
	RetryAfter *time.Duration // Suggested retry delay
}

// Common errors
var (
	ErrRemoteUnavailable = errors.New("remote service unavailable")
	ErrNotImplemented    = errors.New("operation not implemented by this remote")
	ErrInvalidDID        = errors.New("invalid DID format")
	ErrRepoNotFound      = errors.New("repository not found")
)
