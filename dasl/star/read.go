package star

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"unicode/utf8"

	"github.com/hyphacoop/go-dasl/drisl"
)

// Reader is a simple streaming STAR file parser.
//
// Any error encountered while parsing the file is permanent, and additional method calls will return the same error.
type Reader struct {
	// Default is 8 KiByte
	MaxKeySize uint64
	// Default is 5 MiByte
	MaxValueSize uint64
	// Default is 5 MiByte
	MaxMetaSize uint64

	src      *bufio.Reader
	keybuf   []byte
	valbuf   []byte
	meta     []byte
	metaType string
	err      error
}

// NewReader initialize a [Reader] with a buffered I/O reader and default size limits.
//
// This method does not start reading any actual bytes from the provided [io.Reader].
// Note that Reader will eventually read beyond the end of the [io.Reader] to confirm that there are no trailing/padding bytes.
func NewReader(rd io.Reader) *Reader {
	reader := Reader{
		MaxKeySize:   defaultMaxKeySize,
		MaxValueSize: defaultMaxValueSize,
		MaxMetaSize:  defaultMaxMetaSize,
		src:          bufio.NewReader(rd),
	}
	return &reader
}

// Meta returns the meta object bytes from the start of the STAR file.
//
// When initially parsing the meta bytes, will decode as DRISL CBOR to verify encoding, and that '$type' is defined. This may be a duplicate decoding pass.
//
// Returns a cached copy if the meta was already read.
func (r *Reader) Meta() ([]byte, error) {
	if r.meta != nil {
		return r.meta, nil
	}
	if r.err != nil {
		return nil, r.err
	}

	var magic uint32
	if err := binary.Read(r.src, binary.BigEndian, &magic); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		r.err = fmt.Errorf("reading STAR magic header: %w", err)
		return nil, r.err
	}
	if magic != MagicHeader {
		r.err = fmt.Errorf("wrong STAR file magic: %X", magic)
		return nil, r.err
	}

	l, err := binary.ReadUvarint(r.src)
	if err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		r.err = fmt.Errorf("reading STAR meta len varint: %w", err)
		return nil, r.err
	}
	if l > math.MaxInt64 || l > r.MaxMetaSize {
		r.err = fmt.Errorf("STAR meta length too large")
		return nil, r.err
	}
	if l == 0 {
		r.err = fmt.Errorf("empty STAR meta object")
		return nil, r.err
	}

	buf := make([]byte, l)
	if _, err := io.ReadFull(r.src, buf); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		r.err = fmt.Errorf("reading STAR meta: %w", err)
		return nil, r.err
	}

	// deserialize, and check that '$type' is present
	var m metaType
	if err := drisl.Unmarshal(buf, &m); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		r.err = fmt.Errorf("decoding STAR meta CBOR: %w", err)
		return nil, r.err
	}
	if m.Type == nil {
		r.err = fmt.Errorf("missing STAR meta $type")
		return nil, r.err
	}

	r.metaType = *m.Type
	r.meta = buf
	return buf, nil
}

// MetaType returns the '$type' string from header metadata object.
func (r *Reader) MetaType() (string, error) {
	// read the meta if it hasn't been already
	if r.meta == nil {
		if _, err := r.Meta(); err != nil {
			return "", err
		}
	}

	return r.metaType, nil
}

// UnmarshalMeta deserializes the metadata header CBOR object in to a struct or other data container.
func (r *Reader) UnmarshalMeta(ptr any) error {
	// read the meta if it hasn't been already
	if r.meta == nil {
		if _, err := r.Meta(); err != nil {
			return err
		}
	}

	return drisl.Unmarshal(r.meta, ptr)
}

func (r *Reader) readBytes(buf []byte, limit uint64) ([]byte, error) {
	n, err := binary.ReadUvarint(r.src)
	if err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return buf, err
	}
	if n > math.MaxInt64 || n > limit {
		return buf, fmt.Errorf("too large: %d > %d", n, limit)
	}
	if buf == nil || uint64(cap(buf)) < n {
		buf = make([]byte, n)
	}
	buf = buf[:n]
	if _, err := io.ReadFull(r.src, buf); err != nil {
		if err == io.EOF {
			return buf, io.ErrUnexpectedEOF
		}
		return buf, err
	}
	return buf, nil
}

// ReadPair reads the next key/value pair from the file and returns byte slices pointing in to internal buffers.
//
// The internal buffers are reused for subsequent reads (to reduce allocations), meaning that the returned slice data may need to be copied.
//
// Returns [io.EOF] if (and only if) the end of file marker has been reached. Otherwise return a wrapped [io.ErrUnexpectedEOF] or some other more specific error.
func (r *Reader) ReadPair() ([]byte, []byte, error) {
	if r.err != nil {
		return nil, nil, r.err
	}

	// first read meta if it hasn't been
	if r.meta == nil {
		_, err := r.Meta()
		if err != nil {
			return nil, nil, err
		}
	}

	// check the marker byte
	marker, err := r.src.ReadByte()
	if err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		r.err = fmt.Errorf("reading STAR marker: %w", err)
		return nil, nil, r.err
	}
	switch marker {
	case PairMarker:
		// pass
	case EndMarker:
		// verify no further bytes remain
		_, err := r.src.Peek(1)
		if err == nil {
			r.err = fmt.Errorf("additional bytes past end of STAR file")
			return nil, nil, r.err
		} else if err != io.EOF {
			// NOTE: arguably we could just drop I/O errors here once terminal has been reached
			r.err = fmt.Errorf("checking end of STAR file: %w", err)
			return nil, nil, r.err
		}
		// clean end of file
		r.err = io.EOF
		return nil, nil, r.err
	default:
		r.err = fmt.Errorf("unexpected STAR marker byte: 0x%02X", marker)
		return nil, nil, r.err
	}

	r.keybuf, err = r.readBytes(r.keybuf, r.MaxKeySize)
	if err != nil {
		r.err = fmt.Errorf("reading STAR key: %w", err)
		return nil, nil, r.err
	}

	r.valbuf, err = r.readBytes(r.valbuf, r.MaxValueSize)
	if err != nil {
		r.err = fmt.Errorf("reading STAR value: %w", err)
		return nil, nil, r.err
	}

	return r.keybuf, r.valbuf, nil
}

// Scan is an iterator function designed to be used in a `for` loop, similar to [bufio.Scanner].
//
// Designed to be used with [Reader.Key] and [Reader.Value] (to read out values), and [Reader.Err] (to check any intermediate errors).
func (r *Reader) Scan() bool {
	_, _, err := r.ReadPair()
	return err == nil
}

// Key returns a byte slice pointing at an internal buffer for the most recent key. Intended for use with [Reader.Scan].
//
// Returns nil if there was an error, but that should never happen when used with [Reader.Scan].
func (r *Reader) Key() []byte {
	if r.err != nil {
		return nil
	}
	return r.keybuf
}

// Value returns a byte slice pointing at an internal buffer for the most recent value. Intended for use with [Reader.Scan].
//
// Returns nil if there was an error, but that should never happen when used with [Reader.Scan].
func (r *Reader) Value() []byte {
	if r.err != nil {
		return nil
	}
	return r.valbuf
}

// KeyString parses the output of [Reader.Key], validating that the bytes are a valid UTF-8 string. Intended for use with [Reader.Scan].
func (r *Reader) KeyString() (string, error) {
	// guard against misuse of this function
	if r.err != nil {
		return "", r.err
	}
	if !utf8.Valid(r.keybuf) {
		return "", fmt.Errorf("key is not valid UTF-8 string")
	}
	return string(r.keybuf), nil
}

// Err returns any unexpected error encountered while parsing the file. Intended for use with [Reader.Scan].
//
// If file was read to the end successfully, returns 'nil' instead of [io.EOF].
func (r *Reader) Err() error {
	if r.err == io.EOF {
		return nil
	}
	return r.err
}

// Reset clears the internal state of this Reader so that already allocated internal buffers can be reused with a new STAR file.
//
// This is a performance optimization for working with large numbers of STAR files.
func (r *Reader) Reset(rd io.Reader) {
	r.src.Reset(rd)
	r.keybuf = r.keybuf[:0]
	r.valbuf = r.valbuf[:0]
	r.meta = nil
	r.metaType = ""
	r.err = nil
}
