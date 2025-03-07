package repo

import (
	"context"

	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	ipld "github.com/ipfs/go-ipld-format"
)

type TinyBlockstore struct {
	blocks []blocks.Block
}

func (tb *TinyBlockstore) Put(_ context.Context, block blocks.Block) error {
	ncid := block.Cid()
	for i := range tb.blocks {
		if tb.blocks[i].Cid().Equals(ncid) {
			tb.blocks[i] = block
			return nil
		}
	}
	tb.blocks = append(tb.blocks, block)
	return nil
}

func (tb *TinyBlockstore) Get(_ context.Context, ncid cid.Cid) (blocks.Block, error) {
	for i := range tb.blocks {
		if tb.blocks[i].Cid().Equals(ncid) {
			return tb.blocks[i], nil
		}
	}
	return nil, &ipld.ErrNotFound{Cid: ncid}
}
