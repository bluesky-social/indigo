package util

import (
	"context"
	"fmt"

	blockformat "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
)

type ReadRecordBstore struct {
	base blockstore.Blockstore

	set map[cid.Cid]blockformat.Block
}

func NewReadRecordBstore(base blockstore.Blockstore) *ReadRecordBstore {
	return &ReadRecordBstore{
		base: base,
		set:  make(map[cid.Cid]blockformat.Block),
	}
}

var _ blockstore.Blockstore = (*ReadRecordBstore)(nil)

func (bs *ReadRecordBstore) AllReadBlocks() []blockformat.Block {
	var out []blockformat.Block
	for _, v := range bs.set {
		out = append(out, v)
	}
	return out
}

func (bs *ReadRecordBstore) Has(ctx context.Context, c cid.Cid) (bool, error) {
	return bs.base.Has(ctx, c)
}

func (bs *ReadRecordBstore) Get(ctx context.Context, c cid.Cid) (blockformat.Block, error) {
	blk, err := bs.base.Get(ctx, c)
	if err != nil {
		return nil, err
	}

	bs.set[c] = blk

	return blk, nil
}

func (bs *ReadRecordBstore) GetSize(ctx context.Context, c cid.Cid) (int, error) {
	return bs.base.GetSize(ctx, c)
}

func (bs *ReadRecordBstore) DeleteBlock(ctx context.Context, c cid.Cid) error {
	return fmt.Errorf("deletes not allowed on read-record blockstore")
}

func (bs *ReadRecordBstore) Put(context.Context, blockformat.Block) error {
	return fmt.Errorf("writes not allowed on read-record blockstore")
}

func (bs *ReadRecordBstore) PutMany(context.Context, []blockformat.Block) error {
	return fmt.Errorf("writes not allowed on read-record blockstore")
}

func (bs *ReadRecordBstore) AllKeysChan(ctx context.Context) (<-chan cid.Cid, error) {
	return nil, fmt.Errorf("iteration not supported on read-record blockstore")
}

func (bs *ReadRecordBstore) HashOnRead(enabled bool) {

}
