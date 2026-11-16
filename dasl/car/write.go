package car

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/hyphacoop/go-dasl/cid"
	"github.com/hyphacoop/go-dasl/drisl"
)

// Writer is a simple CAR file encoder.
//
// Header roots (if any) be written first, then any number of blocks, then [Writer.Flush] must be called.
type Writer struct {
	// Default is 8 KiByte
	MaxHeaderSize uint64
	// Default is 5 MiByte
	MaxBlockSize uint64

	wroteHeader bool
	out         *bufio.Writer
	err         error
}

// Initializes a new [Writer] with default size limits.
func NewWriter(out io.Writer) *Writer {
	return &Writer{
		MaxHeaderSize: defaultMaxHeaderSize,
		MaxBlockSize:  defaultMaxBlockSize,
		out:           bufio.NewWriter(out),
	}
}

// WriteHeader writes the CAR header CBOR object with the indicated root CIDs (which may be empty).
func (w *Writer) WriteHeader(roots []cid.Cid) error {
	if w.err != nil {
		return w.err
	}
	if w.wroteHeader {
		return fmt.Errorf("already wrote CAR header")
	}

	if roots == nil {
		roots = []cid.Cid{}
	}

	hdr := &Header{
		Version: HeaderVersion,
		Roots:   roots,
	}

	b, err := drisl.Marshal(hdr)
	if err != nil {
		return err
	}

	if uint64(len(b)) > w.MaxHeaderSize {
		return fmt.Errorf("CAR header object too large: %d > %d", len(b), w.MaxHeaderSize)
	}

	if err := w.writeUvarint(len(b)); err != nil {
		w.err = fmt.Errorf("writing CAR header varint: %w", err)
		return w.err
	}

	if _, err := w.out.Write(b); err != nil {
		w.err = fmt.Errorf("writing CAR header object: %w", err)
		return w.err
	}
	w.wroteHeader = true
	return nil
}

// MarshalRootCBOR encodes the provided data object as DASL CBOR, writes the CID of that CBOR as a header root, and then writes the object as a block.
func (w *Writer) MarshalRootCBOR(obj any) error {
	b, err := drisl.Marshal(obj)
	if err != nil {
		return fmt.Errorf("encoding root object CBOR: %w", err)
	}
	digest := sha256.Sum256(b)
	c, err := cid.NewCidFromInfo(cid.CodecDrisl, cid.HashTypeSha256, digest)
	if err != nil {
		return err
	}
	if err := w.WriteHeader([]cid.Cid{c}); err != nil {
		return err
	}
	return w.WriteBlock(c, b)
}

// WriteBlock writes a data block with CID prefix.
//
// If a header was not written before the first WriteBlock call, an empty header will be written.
//
// The CID is not verified against the data.
func (w *Writer) WriteBlock(c cid.Cid, data []byte) error {
	if !c.Defined() {
		return fmt.Errorf("undefined block CID")
	}

	if w.err != nil {
		return w.err
	}
	if !w.wroteHeader {
		// write header first
		if err := w.WriteHeader(nil); err != nil {
			return err
		}
	}

	if uint64(len(data)) > w.MaxBlockSize {
		return fmt.Errorf("CAR block data too large: %d", len(data))
	}

	// length varint then, CID bytes, then data bytes
	if err := w.writeUvarint(len(data) + cid.CidBinaryLength); err != nil {
		w.err = err
		return w.err
	}
	cbuf, err := c.AppendBinary(w.out.AvailableBuffer())
	if err != nil {
		w.err = err
		return w.err
	}
	if _, err := w.out.Write(cbuf); err != nil {
		w.err = err
		return w.err
	}
	if _, err := w.out.Write(data); err != nil {
		w.err = err
		return w.err
	}

	// NOTE: does *not* call w.out.Flush() here
	return nil
}

// MarshalBlockCBOR encodes a data object value to DASL CBOR, and then writes it with the computed CID.
func (w *Writer) MarshalBlockCBOR(data any) error {
	b, err := drisl.Marshal(data)
	if err != nil {
		return fmt.Errorf("encoding value CBOR: %w", err)
	}
	digest := sha256.Sum256(b)
	c, err := cid.NewCidFromInfo(cid.CodecDrisl, cid.HashTypeSha256, digest)
	if err != nil {
		return err
	}
	return w.WriteBlock(c, b)
}

// Flush writes buffered data to the underlying [io.Writer]. It must be called after all blocks have been written to ensure the file is complete, though it may be called at other times as well.
func (w *Writer) Flush() error {
	if err := w.out.Flush(); err != nil {
		w.err = err
		return w.err
	}
	return nil
}

func (w *Writer) writeUvarint(n int) error {
	_, err := w.out.Write(binary.AppendUvarint(w.out.AvailableBuffer(), uint64(n)))
	return err
}
