package spaces

import (
	"encoding/json"
	"io"
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

type ListWritersOptions struct {
	Cursor *string
	Limit  *int64
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
	ExcludeValues *bool
}

type ListBlobsOptions struct {
	Cursor *string
	Limit  *int64
}

type CredentialExchangeOptions struct {
	ClientAttestation *string
	Notify            *NotifyRegistration
}

// NotifyRegistration is renewed by each authorizeSpaceSync call. Keep a unique
// token for each space, stable across authorizations. The caller implements
// confirmRegistration and notifySpaceUpdate at Endpoint.
type NotifyRegistration struct {
	Endpoint string
	Token    string
}

// WriterPage is the authority's discovery report, not a verified inventory. Repos
// are ordered by space revision and may reappear while paging.
type WriterPage struct {
	Repos  []WriterInfo
	Cursor *string
}

type WriterInfo struct {
	Author   syntax.DID
	RepoRev  syntax.TID
	SpaceRev syntax.TID
	Data     [32]byte
	// Removed entries have no RepoRev or Data. Remove this repo from local views.
	Removed bool
}

type OpsPage struct {
	Ops    []Operation
	Cursor *string
	// Head is authenticated on every page. Compare the local set hash only after the last page.
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

// Record contains a MAC-authenticated complete record address and CID, plus
// unmodified JSON. Call VerifyRecord on the value's canonical DAG-CBOR before
// accepting its content; the MAC authenticates the CID, not arbitrary JSON.
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

// RepoDownload is a caller-owned STAR stream. Authentication belongs to this
// response only and must be supplied to VerifyRepo along with the expected repo.
type RepoDownload struct {
	io.ReadCloser
	Authentication *MACVerification
}
