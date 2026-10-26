package spaces

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/spaces/lthash"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
)

const repoArchiveType = "atproto-space-repo"
const maxRepoPathSize = 317 + 1 + 512 // NSID, slash, record key

// RepoVerificationOptions binds a STAR archive to its expected space, author,
// and per-response authentication. MaxBlockSize limits metadata and record bytes
// before allocation. Zero or a value above 1 MiB uses the protocol's 1 MiB limit.
type RepoVerificationOptions struct {
	Space          syntax.SpaceRef
	Author         syntax.DID
	Authentication *MACVerification
	MaxBlockSize   uint64
}

// RepoIndexEntry identifies a record in strict bytewise record-path order.
// Its CID is authenticated only when verification of the entire repo succeeds.
type RepoIndexEntry struct {
	Collection syntax.NSID
	Rkey       syntax.RecordKey
	CID        cid.Cid
}

// RecordBlock is a locally validated record from a MAC-authenticated snapshot.
// CBOR is borrowed, read-only storage valid only during the consume callback;
// consumers retaining it must copy. Collection, Rkey, and CID are value metadata.
// Membership in the complete snapshot is provisional until VerifyRepo succeeds.
type RecordBlock struct {
	Collection syntax.NSID
	Rkey       syntax.RecordKey
	CID        cid.Cid
	CBOR       []byte
}

// RepoVerification is returned only after the complete STAR archive reaches clean
// EOF and its record-set digest matches the authenticated commit. Archives
// never contain blobs.
type RepoVerification struct {
	Commit *Commit
	Index  []RepoIndexEntry
}

// VerifyRepo streams a STAR-lite v1 Spaces archive. It authenticates the commit
// metadata, validates record paths and canonical DRISL, computes record CIDs, and
// compares the complete record set's LtHash digest with the commit's data.
//
// consume may be nil to discard records. It runs after the metadata MAC and each
// record's encoding have been checked, but before the final set-hash comparison.
// Any callback output is provisional until this function succeeds: later parse,
// digest, cancellation, or callback errors invalidate the snapshot. Consumers
// replacing a repo must stage output or roll it back on error. See RecordBlock
// for the borrowed-buffer contract. VerifyRepo does not close src. Cancellation
// cannot interrupt an underlying reader that blocks without context support.
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
	if opts.Authentication == nil {
		return nil, fmt.Errorf("Spaces repo authentication is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limit := opts.MaxBlockSize
	if limit == 0 || limit > atdata.MAX_CBOR_RECORD_SIZE {
		limit = atdata.MAX_CBOR_RECORD_SIZE
	}
	reader := bufio.NewReader(contextReader{ctx, src})
	var magic [4]byte
	if _, err := io.ReadFull(reader, magic[:]); err != nil {
		return nil, fmt.Errorf("reading Spaces STAR magic: %w", err)
	}
	if magic != [4]byte{0x2a, 0x6c, 0x01, 0x00} {
		return nil, fmt.Errorf("invalid Spaces STAR-lite v1 magic")
	}
	metadataSize, err := readSTARLength(reader)
	if err != nil {
		return nil, fmt.Errorf("reading Spaces STAR metadata length: %w", err)
	}
	if metadataSize > limit {
		return nil, fmt.Errorf("Spaces STAR metadata exceeds maximum size")
	}
	buffer := make([]byte, int(metadataSize))
	if _, err := io.ReadFull(reader, buffer); err != nil {
		return nil, fmt.Errorf("reading Spaces STAR metadata: %w", err)
	}
	metadata := bytes.NewReader(buffer)
	commit, err := readCommitCBOR(metadata, repoArchiveType, true)
	if err != nil {
		return nil, err
	}
	if metadata.Len() != 0 {
		return nil, fmt.Errorf("trailing data after Spaces STAR metadata")
	}
	if err := commit.Verify(opts.Space, opts.Author, opts.Authentication); err != nil {
		return nil, err
	}
	var index []RepoIndexEntry
	hash := lthash.New()
	var previous string
	var pathBuffer [maxRepoPathSize]byte
	element := make([]byte, 0, maxRepoPathSize+1+64)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pathSize, err := readSTARLength(reader)
		if err == io.EOF {
			break // EOF is valid only at the beginning of a pair.
		}
		if err != nil {
			return nil, fmt.Errorf("reading Spaces STAR path length: %w", err)
		}
		if pathSize == 0 || pathSize > maxRepoPathSize {
			return nil, fmt.Errorf("invalid Spaces STAR path length: %d", pathSize)
		}
		pathBytes := pathBuffer[:int(pathSize)]
		if _, err := io.ReadFull(reader, pathBytes); err != nil {
			return nil, fmt.Errorf("reading Spaces STAR path: %w", err)
		}
		path := string(pathBytes)
		if previous != "" && path <= previous {
			return nil, fmt.Errorf("Spaces STAR paths are not in strict bytewise order")
		}
		collection, rkey, found := strings.Cut(path, "/")
		if !found || strings.Contains(rkey, "/") {
			return nil, fmt.Errorf("invalid Spaces STAR path: %q", path)
		}
		nsid, err := syntax.ParseNSID(collection)
		if err != nil {
			return nil, fmt.Errorf("invalid Spaces STAR collection: %w", err)
		}
		key, err := syntax.ParseRecordKey(rkey)
		if err != nil {
			return nil, fmt.Errorf("invalid Spaces STAR rkey: %w", err)
		}
		recordSize, err := readSTARLength(reader)
		if err != nil {
			return nil, fmt.Errorf("reading Spaces STAR record length: %w", err)
		}
		if recordSize > limit {
			return nil, fmt.Errorf("Spaces STAR record exceeds maximum size")
		}
		if recordSize > uint64(cap(buffer)) {
			buffer = make([]byte, int(recordSize))
		} else {
			buffer = buffer[:int(recordSize)]
		}
		if _, err := io.ReadFull(reader, buffer); err != nil {
			return nil, fmt.Errorf("reading Spaces STAR record: %w", err)
		}
		if err := validateRecordData(buffer); err != nil {
			return nil, err
		}
		recordCID, err := cid.NewPrefixV1(cid.DagCBOR, multihash.SHA2_256).Sum(buffer)
		if err != nil {
			return nil, err
		}
		index = append(index, RepoIndexEntry{nsid, key, recordCID})
		element = append(element[:0], pathBytes...)
		element = append(element, '/')
		element = append(element, recordCID.String()...)
		hash.Add(element)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if consume != nil {
			if err := consume(RecordBlock{nsid, key, recordCID, buffer}); err != nil {
				return nil, err
			}
		}
		previous = path
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if hash.Sum256() != commit.Data {
		return nil, fmt.Errorf("Spaces STAR record-set LtHash digest does not match commit")
	}
	return &RepoVerification{Commit: commit, Index: index}, nil
}

// STAR lengths use canonical unsigned varints of at most six bytes. Only EOF
// before the first byte is clean; a continuation followed by EOF is truncation.
func readSTARLength(reader io.ByteReader) (uint64, error) {
	var value uint64
	for i := range 6 {
		b, err := reader.ReadByte()
		if err != nil {
			if err == io.EOF && i != 0 {
				err = io.ErrUnexpectedEOF
			}
			return 0, err
		}
		value |= uint64(b&0x7f) << (7 * i)
		if b&0x80 == 0 {
			if i != 0 && b == 0 {
				return 0, fmt.Errorf("noncanonical STAR length varint")
			}
			return value, nil
		}
	}
	return 0, fmt.Errorf("STAR length varint exceeds six bytes")
}
