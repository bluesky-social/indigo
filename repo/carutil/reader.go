package carutil

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"

	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	car "github.com/ipld/go-car"
)

type Reader struct {
	r *bufio.Reader

	bufs [][]byte
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
		r:    r,
		bufs: make([][]byte, 0, 10),
	}, h.Roots[0], nil
}

func (r *Reader) Free(alloc *sync.Pool) {
	for _, b := range r.bufs {
		alloc.Put(b)
	}
	r.bufs = nil
}

const MaxAllowedSectionSize = 32 << 20

func (r *Reader) NextBlock(allocator *sync.Pool, allocMax uint64) (blocks.Block, error) {
	data, err := ldRead(r.r, allocator, allocMax)
	if err != nil {
		return nil, err
	}

	r.bufs = append(r.bufs, data)

	n, c, err := cid.CidFromBytes(data)
	if err != nil {
		return nil, err
	}

	return blocks.NewBlockWithCid(data[n:], c)
}

func ldRead(r *bufio.Reader, alloc *sync.Pool, allocMax uint64) ([]byte, error) {
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

	if l > allocMax {
		// direct allocation, not great
		buf := make([]byte, l)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}

		return buf, nil
	}

	buf := alloc.Get().([]byte)
	buf = buf[:l]

	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}

	return buf, nil
}
