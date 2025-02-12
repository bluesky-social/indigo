package repo

import (
	"context"

	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	ipld "github.com/ipfs/go-ipld-format"
)

type TinyBlockstore struct {
	blocks map[string]blocks.Block
}

func NewTinyBlockstore() *TinyBlockstore {
	return &TinyBlockstore{blocks: make(map[string]blocks.Block, 20)}
}

func (tb *TinyBlockstore) Put(_ context.Context, block blocks.Block) error {
	ncid := block.Cid()
	key := ncid.KeyString()
	tb.blocks[key] = block
	return nil
}

func (tb *TinyBlockstore) Get(_ context.Context, ncid cid.Cid) (blocks.Block, error) {
	key := ncid.KeyString()
	block, found := tb.blocks[key]
	if found {
		return block, nil
	}
	return nil, &ipld.ErrNotFound{Cid: ncid}
}
