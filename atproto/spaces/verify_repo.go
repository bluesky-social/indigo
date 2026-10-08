package spaces

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/atcrypto/lthash"
	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"
	car "github.com/ipld/go-car/v2"
	cbg "github.com/whyrusleeping/cbor-gen"
)

// RepoVerificationOptions binds a CAR to the expected space, author, and trusted
// author signing key. MaxBlockSize limits each block's payload before allocation
// (plus fixed CID framing); zero uses atdata.MAX_CBOR_SIZE. IndexOnly explicitly
// requires a CAR containing only the commit and index, not a truncated full CAR.
type RepoVerificationOptions struct {
	Space        syntax.SpaceRef
	Author       syntax.DID
	PublicKey    atcrypto.PublicKey
	MaxBlockSize uint64
	IndexOnly    bool
}

// RepoIndexEntry identifies one record in canonical index order (shortest path
// first, then bytewise). CID is the authenticated record content address.
type RepoIndexEntry struct {
	Collection syntax.NSID
	Rkey       syntax.RecordKey
	CID        cid.Cid
}

// RecordBlock is a verified record from an authenticated Spaces repo index.
// CBOR is borrowed, read-only storage valid only during the consume callback;
// consumers retaining it must copy. Collection, Rkey, and CID are value metadata.
type RecordBlock struct {
	Collection syntax.NSID
	Rkey       syntax.RecordKey
	CID        cid.Cid
	CBOR       []byte
}

// RepoVerification is returned only after the entire expected CAR reaches clean
// EOF. Commit and Index are authenticated against the supplied space, author, and
// key. RecordsVerified is false only for explicitly requested index-only mode;
// it is true for a fully verified empty repo as well. Blobs are not in repo CARs.
type RepoVerification struct {
	Commit          *Commit
	CommitCID       cid.Cid
	IndexCID        cid.Cid
	Index           []RepoIndexEntry
	RecordsVerified bool
}

// VerifyRepo verifies a v1 Spaces CAR without materializing a blockstore or MST.
// Its two ordered roots and blocks must be commit, index, then every record in
// canonical index order. It checks CID profiles/integrity, commit signature/MAC,
// index paths/order and LtHash digest, record encodings, and exact EOF.
//
// consume may be nil to discard records. It runs only after the commit and entire
// index are authenticated and each supplied record passes verification. However,
// callbacks can have run before a later error (including missing/extra blocks,
// cancellation, or a callback failure). Consumers must treat output as a partial
// verified snapshot until this function succeeds, and stage/roll back a repo
// replacement accordingly. See RecordBlock for the borrowed-buffer contract.
// Index-only mode never invokes consume. VerifyRepo does not close src, and
// cancellation cannot interrupt a blocking reader that does not support it.
func VerifyRepo(ctx context.Context, src io.Reader, opts RepoVerificationOptions, consume func(RecordBlock) error) (*RepoVerification, error) {
	if src == nil {
		return nil, fmt.Errorf("Spaces repo source is required")
	}
	if _, err := syntax.ParseSpaceRef(opts.Space.String()); err != nil {
		return nil, err
	}
	if _, err := syntax.ParseDID(opts.Author.String()); err != nil {
		return nil, err
	}
	if opts.PublicKey == nil {
		return nil, fmt.Errorf("Spaces repo author public key is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limit := opts.MaxBlockSize
	if limit == 0 {
		limit = atdata.MAX_CBOR_SIZE
	}
	// The required CIDv1 + codec + SHA-256 multihash is exactly 36 bytes.
	if limit > math.MaxInt64-36 {
		return nil, fmt.Errorf("Spaces repo block limit is too large")
	}
	counted := &countingReader{reader: contextReader{ctx, src}}
	reader, err := car.NewBlockReader(counted,
		car.MaxAllowedHeaderSize(1024), car.MaxAllowedSectionSize(limit+36),
		car.ZeroLengthSectionAsEOF(false), car.WithTrustedCAR(true))
	if err != nil {
		return nil, fmt.Errorf("reading Spaces CAR header: %w", err)
	}
	if reader.Version != 1 || len(reader.Roots) != 2 {
		return nil, fmt.Errorf("Spaces CAR requires version 1 and two ordered roots")
	}
	for _, root := range reader.Roots {
		if err := validateRecordCID(root); err != nil {
			return nil, fmt.Errorf("invalid Spaces CAR root: %w", err)
		}
	}
	readBlock := func(expected cid.Cid) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		block, err := reader.Next()
		if err != nil {
			return nil, err
		}
		if !block.Cid().Equals(expected) {
			return nil, fmt.Errorf("unexpected Spaces CAR block: expected %s, got %s", expected, block.Cid())
		}
		if uint64(len(block.RawData())) > limit {
			return nil, fmt.Errorf("Spaces CAR block exceeds maximum size")
		}
		return block.RawData(), nil
	}
	commitData, err := readBlock(reader.Roots[0])
	if err != nil {
		return nil, fmt.Errorf("reading Spaces commit block: %w", err)
	}
	if err := verifyBlockIntegrity(commitData, reader.Roots[0]); err != nil {
		return nil, err
	}
	commit, err := DecodeCommitCBOR(commitData)
	if err != nil {
		return nil, err
	}
	if err := commit.Verify(opts.Space, opts.Author, opts.PublicKey); err != nil {
		return nil, err
	}
	indexData, err := readBlock(reader.Roots[1])
	if err != nil {
		return nil, fmt.Errorf("reading Spaces index block: %w", err)
	}
	if err := verifyBlockIntegrity(indexData, reader.Roots[1]); err != nil {
		return nil, err
	}
	index, digest, err := decodeRepoIndex(ctx, indexData)
	if err != nil {
		return nil, err
	}
	if digest != commit.Hash {
		return nil, fmt.Errorf("Spaces index LtHash digest does not match commit")
	}
	if !opts.IndexOnly {
		for _, entry := range index {
			data, err := readBlock(entry.CID)
			if err != nil {
				return nil, fmt.Errorf("reading Spaces record %s/%s: %w", entry.Collection, entry.Rkey, err)
			}
			if err := VerifyRecord(data, entry.CID); err != nil {
				return nil, err
			}
			if consume != nil {
				if err := consume(RecordBlock{entry.Collection, entry.Rkey, entry.CID, data}); err != nil {
					return nil, err
				}
			}
		}
	}
	// The generic reader can return io.EOF after consuming a truncated section.
	// A clean end must consume no bytes at all, rather than merely report EOF.
	before := counted.bytes
	_, err = reader.Next()
	if err != io.EOF {
		if err != nil {
			return nil, fmt.Errorf("reading Spaces CAR end: %w", err)
		}
		return nil, fmt.Errorf("unexpected extra Spaces CAR block")
	}
	if counted.bytes != before {
		return nil, fmt.Errorf("truncated section after Spaces CAR blocks: %w", io.ErrUnexpectedEOF)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &RepoVerification{commit, reader.Roots[0], reader.Roots[1], index, !opts.IndexOnly}, nil
}

func decodeRepoIndex(ctx context.Context, data []byte) ([]RepoIndexEntry, [32]byte, error) {
	var zero [32]byte
	r := bytes.NewReader(data)
	cr := cbg.NewCborReader(r)
	major, count, err := cr.ReadHeader()
	if err != nil {
		return nil, zero, err
	}
	if major != cbg.MajMap {
		return nil, zero, fmt.Errorf("Spaces index must be a DAG-CBOR map")
	}
	// Each profile CID link alone occupies 41 bytes, plus the path header.
	// Bound entry allocation by bytes actually present, not an untrusted count.
	if count > uint64(r.Len())/42 {
		return nil, zero, fmt.Errorf("truncated or oversized Spaces index")
	}
	index := make([]RepoIndexEntry, 0, int(count))
	hash := lthash.New()
	var previous string
	element := make([]byte, 0, 1024)
	for i := range count {
		if err := ctx.Err(); err != nil {
			return nil, zero, err
		}
		path, err := cbg.ReadStringWithMax(cr, atdata.MAX_OBJECT_KEY_LEN)
		if err != nil {
			return nil, zero, err
		}
		if i != 0 && (len(path) < len(previous) || (len(path) == len(previous) && path <= previous)) {
			return nil, zero, fmt.Errorf("Spaces index paths are not in strict canonical order")
		}
		collection, rkey, found := strings.Cut(path, "/")
		if !found || strings.Contains(rkey, "/") {
			return nil, zero, fmt.Errorf("invalid Spaces index path: %q", path)
		}
		nsid, err := syntax.ParseNSID(collection)
		if err != nil {
			return nil, zero, fmt.Errorf("invalid Spaces index collection: %w", err)
		}
		key, err := syntax.ParseRecordKey(rkey)
		if err != nil {
			return nil, zero, fmt.Errorf("invalid Spaces index rkey: %w", err)
		}
		c, err := cbg.ReadCid(cr)
		if err != nil {
			return nil, zero, err
		}
		if err := validateRecordCID(c); err != nil {
			return nil, zero, err
		}
		index = append(index, RepoIndexEntry{nsid, key, c})
		element = append(element[:0], path...)
		element = append(element, '/')
		element = append(element, c.String()...)
		hash.Add(element)
		previous = path
	}
	if r.Len() != 0 {
		return nil, zero, fmt.Errorf("trailing data after Spaces index")
	}
	return index, hash.Sum256(), nil
}

type countingReader struct {
	reader io.Reader
	bytes  uint64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.bytes += uint64(n)
	return n, err
}
