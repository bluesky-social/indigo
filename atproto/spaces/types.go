package spaces

import (
	"encoding/json"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"
)

// ClientOptions configures identity resolution and HTTP transport. Headers are
// copied by NewClient; subsequent changes to the supplied header map have no effect.
type ClientOptions struct {
	Directory  identity.Directory
	HTTPClient *http.Client
	Headers    http.Header
}

type ListReposOptions struct {
	Cursor *string
	Limit  *int64
}

type GetRepoOptions struct {
	ExcludeValues *bool
}

type ListOpsOptions struct {
	Since         *syntax.TID
	Cursor        *string
	Limit         *int64
	ExcludeValues *bool
}

type ListRecordsOptions struct {
	Collection    *syntax.NSID
	Cursor        *string
	Limit         *int64
	Reverse       *bool
	ExcludeValues *bool
}

type ListBlobsOptions struct {
	Since  *syntax.TID
	Cursor *string
	Limit  *int64
}

type CredentialExchangeOptions struct {
	ClientAttestation *string
}

// RepoPage is the authority's discovery report, not a verified inventory. Repos
// are ordered by space revision and may reappear while paging.
type RepoPage struct {
	Repos  []RepoInfo
	Cursor *string
}

type RepoInfo struct {
	Author   syntax.DID
	RepoRev  syntax.TID
	SpaceRev syntax.TID
	Hash     [32]byte
}

type OpsPage struct {
	Ops    []Operation
	Cursor *string
	// Head is absent on intermediate pages of the operation log.
	Head *Commit
}

// Operation retains the revision identifying its atomic batch. A nil CID is a
// delete and a nil Prev is a create. Value may be absent for superseded records,
// deletes, or metadata-only requests; no record decoding is performed.
type Operation struct {
	Revision   syntax.TID
	Collection syntax.NSID
	Rkey       syntax.RecordKey
	CID        *cid.Cid
	Prev       *cid.Cid
	Value      json.RawMessage
}

// Record contains the returned complete Spaces record address and unmodified
// JSON value. Retrieval does not verify the record against its CID.
type Record struct {
	URI   syntax.SpaceURI
	CID   cid.Cid
	Value json.RawMessage
}

type RecordPage struct {
	Records []RecordEntry
	Cursor  *string
}

type RecordEntry struct {
	Collection syntax.NSID
	Rkey       syntax.RecordKey
	CID        cid.Cid
	Value      json.RawMessage
}

type BlobPage struct {
	CIDs   []cid.Cid
	Cursor *string
}
