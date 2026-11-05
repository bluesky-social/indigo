package spaces

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
)

func parseRecordCID(value string) (cid.Cid, error) {
	c, err := cid.Decode(value)
	if err != nil {
		return cid.Undef, fmt.Errorf("invalid record CID: %w", err)
	}
	if err := validateRecordCID(c); err != nil {
		return cid.Undef, err
	}
	return c, nil
}

func parseBlobCID(value string) (cid.Cid, error) {
	c, err := cid.Decode(value)
	if err != nil {
		return cid.Undef, fmt.Errorf("invalid blob CID: %w", err)
	}
	if err := validateBlobCID(c); err != nil {
		return cid.Undef, err
	}
	return c, nil
}

func validateRecordCID(c cid.Cid) error {
	return validateCIDProfile(c, cid.DagCBOR)
}

func validateBlobCID(c cid.Cid) error {
	return validateCIDProfile(c, cid.Raw)
}

func validateCIDProfile(c cid.Cid, codec uint64) error {
	if !c.Defined() {
		return fmt.Errorf("undefined Spaces CID")
	}
	prefix := c.Prefix()
	if prefix.Version != 1 || prefix.Codec != codec || prefix.MhType != multihash.SHA2_256 || prefix.MhLength != 32 {
		return fmt.Errorf("Spaces CID requires CIDv1 codec 0x%x and SHA-256 with a 32-byte digest", codec)
	}
	return nil
}

func verifyBlockIntegrity(data []byte, expected cid.Cid) error {
	if err := validateRecordCID(expected); err != nil {
		return err
	}
	actual, err := expected.Prefix().Sum(data)
	if err != nil {
		return err
	}
	if !actual.Equals(expected) {
		return fmt.Errorf("Spaces block CID integrity mismatch: %s", expected)
	}
	return nil
}

// VerifyRecord checks a record's CIDv1/DAG-CBOR/SHA-256 profile, content digest,
// and canonical atproto data-model encoding. It does not check a Lexicon schema
// or establish that the record belongs to a particular repo; VerifyRepo binds
// the complete record set to an authenticated commit.
func VerifyRecord(data []byte, expected cid.Cid) error {
	if err := verifyBlockIntegrity(data, expected); err != nil {
		return err
	}
	return validateRecordData(data)
}

func validateRecordData(data []byte) error {
	obj, err := atdata.UnmarshalCBOR(data)
	if err != nil {
		return fmt.Errorf("invalid Spaces record data: %w", err)
	}
	canonical, err := atdata.MarshalCBOR(obj)
	if err != nil {
		return err
	}
	if !bytes.Equal(data, canonical) {
		return fmt.Errorf("Spaces record is not canonical DAG-CBOR")
	}
	return nil
}

// VerifyBlob copies a blob from src to dst while checking cancellation and its
// CIDv1/raw/SHA-256 content digest. Neither stream is closed. Bytes written to
// dst are provisional until this function returns nil: an error can leave
// partial or corrupt output, which the caller must discard or roll back.
// Cancellation is checked between reads and writes; it cannot interrupt an
// underlying reader or writer that itself blocks without supporting context.
func VerifyBlob(ctx context.Context, src io.Reader, dst io.Writer, expected cid.Cid) error {
	if err := validateBlobCID(expected); err != nil {
		return err
	}
	if src == nil || dst == nil {
		return fmt.Errorf("Spaces blob source and destination are required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	h := sha256.New()
	var buffer [32 * 1024]byte
	if _, err := io.CopyBuffer(io.MultiWriter(contextWriter{ctx, dst}, h), contextReader{ctx, src}, buffer[:]); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var digest [32]byte
	h.Sum(digest[:0])
	mh, err := multihash.Encode(digest[:], multihash.SHA2_256)
	if err != nil {
		return err
	}
	if !cid.NewCidV1(cid.Raw, mh).Equals(expected) {
		return fmt.Errorf("Spaces blob CID integrity mismatch: %s", expected)
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	if canceled := r.ctx.Err(); canceled != nil {
		return n, canceled
	}
	return n, err
}

type contextWriter struct {
	ctx    context.Context
	writer io.Writer
}

func (w contextWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := w.writer.Write(p)
	if canceled := w.ctx.Err(); canceled != nil {
		return n, canceled
	}
	return n, err
}
