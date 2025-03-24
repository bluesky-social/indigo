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
	blk, _, err := r.NextBlockBuf(nil)
	if err != nil {
		return nil, err
	}

	return blk, nil
}

func (r *Reader) NextBlockBuf(buf []byte) (*BasicBlock, bool, error) {
	data, usedBuf, err := ldRead(r.r, buf)
	if err != nil {
		return nil, false, err
	}

	n, c, err := cid.CidFromBytes(data)
	if err != nil {
		return nil, false, err
	}

	return NewBlockWithCid(data[n:], data, c), usedBuf, nil
}

// reads a length delimited value off of the given reader into the the given buf if its big enough, otherwise allocates a new buffer.
// returns whether or not the passed in buffer was used
func ldRead(r *bufio.Reader, buf []byte) ([]byte, bool, error) {
	if _, err := r.Peek(1); err != nil { // no more blocks, likely clean io.EOF
		return nil, false, err
	}

	l, err := binary.ReadUvarint(r)
	if err != nil {
		if err == io.EOF {
			return nil, false, io.ErrUnexpectedEOF // don't silently pretend this is a clean EOF
		}
		return nil, false, err
	}

	if l > uint64(MaxAllowedSectionSize) { // Don't OOM
		return nil, false, errors.New("malformed car; header is bigger than util.MaxAllowedSectionSize")
	}

	if l > uint64(len(buf)) {
		// direct allocation, not great
		buf := make([]byte, l)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, false, err
		}

		return buf, false, nil
	}

	buf = buf[:l]
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, false, err
	}

	return buf, true, nil
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
