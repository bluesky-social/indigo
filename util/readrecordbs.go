package util

import (
	"context"
	"fmt"

	blockformat "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
)

type LoggingBstore struct {
	base blockstore.Blockstore

	set map[cid.Cid]blockformat.Block
}

func NewLoggingBstore(base blockstore.Blockstore) *LoggingBstore {
	return &LoggingBstore{
		base: base,
		set:  make(map[cid.Cid]blockformat.Block),
	}
}

var _ blockstore.Blockstore = (*LoggingBstore)(nil)

func (bs *LoggingBstore) GetLoggedBlocks() []blockformat.Block {
	var out []blockformat.Block
	for _, v := range bs.set {
		out = append(out, v)
	}
	return out
}

func (bs *LoggingBstore) Has(ctx context.Context, c cid.Cid) (bool, error) {
	return bs.base.Has(ctx, c)
}

func (bs *LoggingBstore) Get(ctx context.Context, c cid.Cid) (blockformat.Block, error) {
	blk, err := bs.base.Get(ctx, c)
	if err != nil {
		return nil, err
	}

	bs.set[c] = blk

	return blk, nil
}

func (bs *LoggingBstore) GetSize(ctx context.Context, c cid.Cid) (int, error) {
	return bs.base.GetSize(ctx, c)
}

func (bs *LoggingBstore) DeleteBlock(ctx context.Context, c cid.Cid) error {
	return fmt.Errorf("deletes not allowed on logging blockstore")
}

func (bs *LoggingBstore) Put(context.Context, blockformat.Block) error {
	return fmt.Errorf("writes not allowed on logging blockstore")
}

func (bs *LoggingBstore) PutMany(context.Context, []blockformat.Block) error {
	return fmt.Errorf("writes not allowed on logging blockstore")
}

func (bs *LoggingBstore) AllKeysChan(ctx context.Context) (<-chan cid.Cid, error) {
	return nil, fmt.Errorf("iteration not allowed on logging blockstore")
}

func (bs *LoggingBstore) HashOnRead(enabled bool) {

}
