package car

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"

	"github.com/hyphacoop/go-dasl/cid"
	"github.com/hyphacoop/go-dasl/drisl"
)

// Reader is a simple streaming CAR file parser.
//
// Any I/O errors encountered while parsing the file is permanent, and additional method calls will return the same error.
type Reader struct {
	// Default is 8 KiByte
	MaxHeaderSize uint64
	// Default is 5 MiByte
	MaxBlockSize uint64
	// Skip verification of block content hash (CID). This is unsafe unless calling code always verifies integrity separately.
	SkipIntegrityCheck bool

	src     *bufio.Reader
	cidbuf  []byte
	databuf []byte
	header  *Header
	err     error
}

// NewReader initialize a [Reader] with a buffered I/O reader and default size limits.
//
// This method does not start reading any actual bytes from the provided [io.Reader].
func NewReader(rd io.Reader) *Reader {
	reader := Reader{
		MaxHeaderSize: defaultMaxHeaderSize,
		MaxBlockSize:  defaultMaxBlockSize,
		src:           bufio.NewReader(rd),
	}
	return &reader
}

// Header returns the header from the start of the CAR file as a struct.
//
// Returns a cached copy if the header was already read.
func (r *Reader) Header() (*Header, error) {
	if r.header != nil {
		return r.header, nil
	}
	if r.err != nil {
		return nil, r.err
	}

	l, err := binary.ReadUvarint(r.src)
	if err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		r.err = fmt.Errorf("reading CAR header len varint: %w", err)
		return nil, r.err
	}
	if l > math.MaxInt64 || l > r.MaxHeaderSize {
		r.err = fmt.Errorf("CAR header length too large")
		return nil, r.err
	}
	if l == 0 {
		r.err = fmt.Errorf("empty CAR header object")
		return nil, r.err
	}

	buf := make([]byte, l)
	if _, err := io.ReadFull(r.src, buf); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		r.err = fmt.Errorf("reading CAR header: %w", err)
		return nil, r.err
	}

	var hdr Header
	if err := drisl.Unmarshal(buf, &hdr); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		r.err = fmt.Errorf("decoding CAR header CBOR: %w", err)
		return nil, r.err
	}
	if hdr.Version != HeaderVersion {
		r.err = fmt.Errorf("wrong CAR header version: %d", hdr.Version)
		return nil, r.err
	}
	// NOTE: this is different from an empty array (which is allowed)
	if hdr.Roots == nil {
		r.err = fmt.Errorf("invalid CAR header: missing roots array")
		return nil, r.err
	}

	r.header = &hdr
	return &hdr, nil
}

// ReadBlock reads the next CID/data block from the file and returns the CID and a byte slice pointing to an internal buffer.
//
// The returned data bytes slice points in to an internal buffer that will be reused for the next block, to reduce allocations. Calling code must copy this data to persist it. The returned CID is a copied structure and can be persisted as-is.
//
// If an error is encountered, the returned [cid.Cid] is empty ("undefined").
//
// Returns [io.EOF] if (and only if) the end of file marker has been reached. Otherwise return a wrapped [io.ErrUnexpectedEOF] or some other more specific error.
func (r *Reader) ReadBlock() (cid.Cid, []byte, error) {
	var c cid.Cid
	if r.err != nil {
		return c, nil, r.err
	}

	// first read header if it hasn't been
	if r.header == nil {
		_, err := r.Header()
		if err != nil {
			return c, nil, err
		}
	}

	n, err := binary.ReadUvarint(r.src)
	if err != nil {
		// this is the one place io.EOF is allowed
		r.err = err
		return c, nil, r.err
	}
	if n < cid.CidBinaryLength {
		r.err = fmt.Errorf("block CID prefix too short: %d", n)
		return c, nil, r.err
	}
	n = n - cid.CidBinaryLength
	if n > math.MaxInt64 || n > r.MaxBlockSize {
		r.err = fmt.Errorf("block too large: %d > %d", n, r.MaxBlockSize)
		return c, nil, r.err
	}

	// read CID
	if r.cidbuf == nil || cap(r.cidbuf) < cid.CidBinaryLength {
		r.cidbuf = make([]byte, cid.CidBinaryLength)
	}
	buf := r.cidbuf[:cid.CidBinaryLength]
	if _, err := io.ReadFull(r.src, buf); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		r.err = fmt.Errorf("reading block CID: %w", err)
		return c, nil, r.err
	}
	r.cidbuf = buf
	c, err = cid.NewCidFromBytes(r.cidbuf)
	if err != nil {
		r.err = fmt.Errorf("invalid block CID: %w", err)
		return c, nil, r.err
	}

	// read data
	if r.databuf == nil || uint64(cap(r.databuf)) < n {
		r.databuf = make([]byte, n)
	}
	buf = r.databuf[:n]
	if _, err := io.ReadFull(r.src, buf); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		r.err = fmt.Errorf("reading block data: %w", err)
		return c, nil, r.err
	}
	r.databuf = buf

	if !r.SkipIntegrityCheck {
		// verify data matches CID
		if !c.VerifyBytes(buf) {
			// TODO: should this be a special per-block error, instead of erroring out the entire file?
			r.err = fmt.Errorf("block data integrity failed")
			return c, nil, r.err
		}
	}

	return c, r.databuf, nil
}

// Scan is an iterator function designed to be used in a `for` loop, similar to [bufio.Scanner].
//
// Designed to be used with [Reader.CID] and [Reader.Data] (to read out values), and [Reader.Err] (to check any intermediate errors).
func (r *Reader) Scan() bool {
	_, _, err := r.ReadBlock()
	return err == nil
}

// CID returns a [cid.Cid] of the current block. Intended for use with [Reader.Scan].
//
// The returned [cid.Cid] is an allocated copy and can be stored and reused.
//
// Returns an empty CID ("undefined") if there was an error, but that should never happen when used with [Reader.Scan].
func (r *Reader) CID() cid.Cid {
	if r.err != nil || r.cidbuf == nil {
		return cid.Cid{}
	}
	c, err := cid.NewCidFromBytes(r.cidbuf)
	if err != nil {
		return cid.Cid{}
	}
	return c
}

// Data returns a byte slice pointing at an internal buffer for the most recent block data. Intended for use with [Reader.Scan].
//
// Returns nil if there was an error, but that should never happen when used with [Reader.Scan].
func (r *Reader) Data() []byte {
	if r.err != nil {
		return nil
	}
	return r.databuf
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

// Reset clears the internal state of this Reader so that already allocated internal buffer can be reused with a new CAR file.
//
// This is a performance optimization for working with large numbers of CAR files.
func (r *Reader) Reset(rd io.Reader) {
	r.src.Reset(rd)
	r.cidbuf = r.cidbuf[:0]
	r.databuf = r.databuf[:0]
	r.header = nil
	r.err = nil
}
