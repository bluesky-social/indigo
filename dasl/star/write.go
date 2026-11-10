package star

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/hyphacoop/go-dasl/drisl"
)

// Writer is a simple STAR file encoder.
//
// Meta header must be written first, then any number of pairs (may be none), then [Writer.Close] must be called to end the file.
type Writer struct {
	// Default is 8 KiByte
	MaxKeySize uint64
	// Default is 5 MiByte
	MaxValueSize uint64
	// Default is 5 MiByte
	MaxMetaSize uint64

	wroteMeta bool
	wroteEnd  bool
	out       *bufio.Writer
	err       error
}

// Initializes a new [Writer] with default size limits.
func NewWriter(out io.Writer) *Writer {
	return &Writer{
		MaxKeySize:   defaultMaxKeySize,
		MaxValueSize: defaultMaxValueSize,
		MaxMetaSize:  defaultMaxMetaSize,
		out:          bufio.NewWriter(out),
	}
}

// WriteMeta writes the magic file header and then metadata object bytes to the buffered output.
//
// The supplied bytes MUST be valid DASL CBOR, and MUST contain a '$type' string field. This method does not verify these requirements.
func (w *Writer) WriteMeta(meta []byte) error {
	if w.err != nil {
		return w.err
	}
	if w.wroteMeta {
		return fmt.Errorf("already wrote STAR meta")
	}
	if uint64(len(meta)) == 0 {
		return fmt.Errorf("empty STAR meta object")
	}

	if uint64(len(meta)) > w.MaxMetaSize {
		return fmt.Errorf("STAR meta object too large: %d > %d", len(meta), w.MaxMetaSize)
	}

	if err := binary.Write(w.out, binary.BigEndian, MagicHeader); err != nil {
		w.err = fmt.Errorf("writing STAR magic header: %w", err)
		return w.err
	}

	if err := w.writeUvarint(len(meta)); err != nil {
		w.err = fmt.Errorf("writing STAR meta varint: %w", err)
		return w.err
	}

	if _, err := w.out.Write(meta); err != nil {
		w.err = fmt.Errorf("writing STAR meta object: %w", err)
		return w.err
	}
	w.wroteMeta = true
	return nil
}

// MarshalMeta encodes the provided data object as DASL CBOR and writes it as the metadata header (using [Writer.WriteMeta]).
//
// The supplied data object MUST contain a '$type' string field. This method does not verify that requirement.
func (w *Writer) MarshalMeta(meta any) error {
	b, err := drisl.Marshal(meta)
	if err != nil {
		return fmt.Errorf("encoding meta CBOR: %w", err)
	}
	return w.WriteMeta(b)
}

// WritePair writes a key/value pair to the buffered output.
func (w *Writer) WritePair(key []byte, val []byte) error {
	if w.err != nil {
		return w.err
	}
	if !w.wroteMeta {
		return fmt.Errorf("must write STAR meta before any pairs")
	}
	if w.wroteEnd {
		return fmt.Errorf("STAR end marker has already been written")
	}

	if uint64(len(key)) > w.MaxKeySize {
		return fmt.Errorf("STAR key too large: %d", len(key))
	}
	if uint64(len(val)) > w.MaxValueSize {
		return fmt.Errorf("STAR val too large: %d", len(val))
	}

	// pair marker byte
	if err := w.out.WriteByte(PairMarker); err != nil {
		w.err = err
		return w.err
	}

	// key: length varint then key bytes
	if err := w.writeUvarint(len(key)); err != nil {
		w.err = err
		return w.err
	}
	if _, err := w.out.Write(key); err != nil {
		w.err = err
		return w.err
	}

	// value: length varint then val bytes
	if err := w.writeUvarint(len(val)); err != nil {
		w.err = err
		return w.err
	}
	if _, err := w.out.Write(val); err != nil {
		w.err = err
		return w.err
	}

	// NOTE: does *not* call w.out.Flush() here
	return nil
}

// MarshalPairCBOR encodes a data object value to DASL CBOR, and then writes it as a pair with the provided key.
func (w *Writer) MarshalPairCBOR(key []byte, value any) error {
	b, err := drisl.Marshal(value)
	if err != nil {
		return fmt.Errorf("encoding value CBOR: %w", err)
	}
	return w.WritePair(key, b)
}

// Close is mandatory, and must be called after meta header and all pairs have been written. It writes a terminating marker, then flushes any buffered I/O to the original [io.Writer].
//
// This method can be called redundantly without an error or additional write. Any other writes will error after calling this method.
//
// Does not close the underlying [io.Writer].
func (w *Writer) Close() error {
	if w.err != nil {
		return w.err
	}
	if !w.wroteMeta {
		return fmt.Errorf("must write STAR meta before ending file")
	}
	if w.wroteEnd {
		return nil
	}
	if err := w.out.WriteByte(EndMarker); err != nil {
		w.err = err
		return w.err
	}
	w.wroteEnd = true
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
