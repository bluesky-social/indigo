package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// API for clients which pull data from the public atproto network.
//
// Implementations of this interface might resolve PDS instances for DIDs, and fetch data from there. Or they might talk to an archival relay or other network mirroring service.
type NetworkClient interface {
	// Fetches record JSON, without verification or validation. A version (CID) can optionally be specified; use empty string to fetch the latest.
	// Returns the record as JSON, and the CID indicated by the server. Does not verify that the data (as CBOR) matches the CID, and does not cryptographically verify a "proof chain" to the record.
	GetRecordJSON(ctx context.Context, aturi syntax.ATURI, version syntax.CID) (*json.RawMessage, *syntax.CID, error)

	// Fetches the indicated record as CBOR, and authenticates it by checking both the cryptographic signature and Merkle Tree hashes from the current repo revision. A version (CID) can optionally be specified; use empty string to fetch the latest.
	// Returns the record as CBOR; the CID of the validated record, and the repo commit revision.
	VerifyRecordCBOR(ctx context.Context, aturi syntax.ATURI, version syntax.CID) (*[]byte, *syntax.CID, string, error)

	// Fetches repo export (CAR file). Optionally attempts to fetch only the diff "since" an earlier repo revision.
	GetRepoCAR(ctx context.Context, did syntax.DID, since string) (*io.Reader, error)

	// Fetches indicated blob. Does not validate the CID. Returns a reader (which calling code is responsible for closing).
	GetBlob(ctx context.Context, did syntax.DID, cid syntax.CID) (*io.ReadCloser, error)
	GetAccountStatus(ctx context.Context, did syntax.DID) (*AccountStatus, error)
}

// XXX: type alias to codegen? or just copy? this is protocol-level
type AccountStatus struct {
}

func VerifyBlobCID(blob []byte, cid syntax.CID) error {
	// XXX: compute hash, check against provided CID
	return errors.New("Not Implemented")
}
