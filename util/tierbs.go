package util

import (
	"context"
	"fmt"

	blockformat "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	ipld "github.com/ipfs/go-ipld-format"
)

type ReadThroughBstore struct {
	base  blockstore.Blockstore
	fresh blockstore.Blockstore
}

func NewReadThroughBstore(base, fresh blockstore.Blockstore) *ReadThroughBstore {
	return &ReadThroughBstore{
		base:  base,
		fresh: fresh,
	}
}

var _ blockstore.Blockstore = (*ReadThroughBstore)(nil)

func (bs *ReadThroughBstore) DeleteBlock(ctx context.Context, c cid.Cid) error {
	return bs.fresh.DeleteBlock(ctx, c)

}

func (bs *ReadThroughBstore) Has(ctx context.Context, c cid.Cid) (bool, error) {
	h, err := bs.fresh.Has(ctx, c)
	if err != nil {
		return false, err
	}

	if h {
		return true, nil
	}

	return bs.base.Has(ctx, c)
}

func (bs *ReadThroughBstore) Get(ctx context.Context, c cid.Cid) (blockformat.Block, error) {
	blk, err := bs.fresh.Get(ctx, c)
	if err == nil {
		return blk, nil
	}

	if !ipld.IsNotFound(err) {
		return nil, err
	}

	return bs.base.Get(ctx, c)
}

func (bs *ReadThroughBstore) GetSize(ctx context.Context, c cid.Cid) (int, error) {
	size, err := bs.fresh.GetSize(ctx, c)
	if err == nil {
		return size, nil
	}

	if !ipld.IsNotFound(err) {
		return -1, err
	}

	return bs.base.GetSize(ctx, c)
}

func (bs *ReadThroughBstore) Put(context.Context, blockformat.Block) error {
	return fmt.Errorf("writes not allows on readthrough blockstore")
}

func (bs *ReadThroughBstore) PutMany(context.Context, []blockformat.Block) error {
	return fmt.Errorf("writes not allows on readthrough blockstore")
}

func (bs *ReadThroughBstore) AllKeysChan(ctx context.Context) (<-chan cid.Cid, error) {
	return nil, fmt.Errorf("iteration not supported on readthrough blockstore")
}

func (bs *ReadThroughBstore) HashOnRead(enabled bool) {

}
