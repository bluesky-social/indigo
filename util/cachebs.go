package util

import (
	"context"
	"fmt"

	lru "github.com/hashicorp/golang-lru"
	blockformat "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
)

type CacheBlockstore struct {
	base  blockstore.Blockstore
	cache *lru.TwoQueueCache
}

func NewCacheBlockstore(base blockstore.Blockstore, cache *lru.TwoQueueCache) *CacheBlockstore {
	return &CacheBlockstore{
		base:  base,
		cache: cache,
	}
}

func (bs *CacheBlockstore) GetCache() *lru.TwoQueueCache {
	return bs.cache
}

var _ blockstore.Blockstore = (*CacheBlockstore)(nil)

func (bs *CacheBlockstore) DeleteBlock(ctx context.Context, c cid.Cid) error {
	bs.cache.Remove(c.KeyString())
	return bs.base.DeleteBlock(ctx, c)
}

func (bs *CacheBlockstore) Has(ctx context.Context, c cid.Cid) (bool, error) {
	_, ok := bs.cache.Get(c.KeyString())
	if ok {
		return true, nil
	}

	return bs.base.Has(ctx, c)
}

func (bs *CacheBlockstore) Get(ctx context.Context, c cid.Cid) (blockformat.Block, error) {
	v, ok := bs.cache.Get(c.KeyString())
	if ok {
		return v.(blockformat.Block), nil
	}

	blk, err := bs.base.Get(ctx, c)
	if err != nil {
		return nil, err
	}

	bs.cache.Add(c.KeyString(), blk)
	return blk, nil
}

func (bs *CacheBlockstore) GetSize(ctx context.Context, c cid.Cid) (int, error) {
	return bs.base.GetSize(ctx, c)
}

func (bs *CacheBlockstore) Put(ctx context.Context, blk blockformat.Block) error {
	if err := bs.base.Put(ctx, blk); err != nil {
		return err
	}

	bs.cache.Add(blk.Cid().KeyString(), blk)
	return nil
}

func (bs *CacheBlockstore) PutMany(context.Context, []blockformat.Block) error {
	return fmt.Errorf("writes not allows on caching blockstore")
}

func (bs *CacheBlockstore) AllKeysChan(ctx context.Context) (<-chan cid.Cid, error) {
	return nil, fmt.Errorf("iteration not supported on caching blockstore")
}

func (bs *CacheBlockstore) HashOnRead(enabled bool) {

}
