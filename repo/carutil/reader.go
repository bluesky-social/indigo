package carutil

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/ipfs/go-cid"
	car "github.com/ipld/go-car"
)

type Reader struct {
	r *bufio.Reader
}

func NewReader(r *bufio.Reader) (*Reader, cid.Cid, error) {
	h, err := car.ReadHeader(r)
	if err != nil {
		return nil, cid.Undef, err
	}

	if h.Version != 1 {
		return nil, cid.Undef, fmt.Errorf("invalid version: %d", h.Version)
	}

	if len(h.Roots) != 1 {
		return nil, cid.Undef, fmt.Errorf("expected only 1 root in car file")
	}

	return &Reader{
		r: r,
	}, h.Roots[0], nil
}

const MaxAllowedSectionSize = 32 << 20

func (r *Reader) NextBlock() (*BasicBlock, error) {
	data, err := ldRead(r.r)
	if err != nil {
		return nil, err
	}

	n, c, err := cid.CidFromBytes(data)
	if err != nil {
		return nil, err
	}

	return NewBlockWithCid(data[n:], data, c), nil
}

func ldRead(r *bufio.Reader) ([]byte, error) {
	if _, err := r.Peek(1); err != nil { // no more blocks, likely clean io.EOF
		return nil, err
	}

	l, err := binary.ReadUvarint(r)
	if err != nil {
		if err == io.EOF {
			return nil, io.ErrUnexpectedEOF // don't silently pretend this is a clean EOF
		}
		return nil, err
	}

	if l > uint64(MaxAllowedSectionSize) { // Don't OOM
		return nil, errors.New("malformed car; header is bigger than util.MaxAllowedSectionSize")
	}

	// direct allocation, not great
	buf := make([]byte, l)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}

	return buf, nil
}

type BasicBlock struct {
	cid  cid.Cid
	data []byte
	base []byte
}

func NewBlockWithCid(data, base []byte, c cid.Cid) *BasicBlock {
	return &BasicBlock{data: data, cid: c, base: base}
}

// RawData returns the block raw contents as a byte slice.
func (b *BasicBlock) RawData() []byte {
	return b.data
}

// Cid returns the content identifier of the block.
func (b *BasicBlock) Cid() cid.Cid {
	return b.cid
}

// String provides a human-readable representation of the block CID.
func (b *BasicBlock) String() string {
	return fmt.Sprintf("[Block %s]", b.Cid())
}

// Loggable returns a go-log loggable item.
func (b *BasicBlock) Loggable() map[string]interface{} {
	return map[string]interface{}{
		"block": b.Cid().String(),
	}
}

func (b *BasicBlock) BaseBuffer() []byte {
	return b.base
}
