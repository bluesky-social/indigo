package repo

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/bluesky-social/indigo/mst"
	"github.com/bluesky-social/indigo/repo/carutil"
	"github.com/bluesky-social/indigo/util"
	block "github.com/ipfs/go-block-format"
	cid "github.com/ipfs/go-cid"
	"go.opentelemetry.io/otel"
)

type waitingBlockstore struct {
	lk             sync.Mutex
	blockWaits     map[cid.Cid]chan block.Block
	otherBlocks    map[cid.Cid]block.Block
	streamComplete bool
}

func newWaitingBlockstore() *waitingBlockstore {
	return &waitingBlockstore{
		blockWaits:  make(map[cid.Cid]chan block.Block),
		otherBlocks: make(map[cid.Cid]block.Block),
	}
}

func (bs *waitingBlockstore) Get(ctx context.Context, cc cid.Cid) (block.Block, error) {
	bs.lk.Lock()

	if blk, ok := bs.otherBlocks[cc]; ok {
		delete(bs.otherBlocks, cc)
		bs.lk.Unlock()
		return blk, nil
	}

	if bs.streamComplete {
		bs.lk.Unlock()
		return nil, ErrMissingBlock
	}

	bw, ok := bs.blockWaits[cc]
	if ok {
		bs.lk.Unlock()
		return nil, fmt.Errorf("somehow already have active wait for block in question: %s", cc)
	}

	bw = make(chan block.Block, 1)

	bs.blockWaits[cc] = bw

	bs.lk.Unlock()

	select {
	case blk, ok := <-bw:
		if !ok {
			return nil, ErrMissingBlock
		}

		return blk, nil
	case <-ctx.Done():
		return nil, ctx.Err()

	}
}

var ErrMissingBlock = fmt.Errorf("block was missing from archive")

func (bs *waitingBlockstore) Put(ctx context.Context, blk block.Block) error {
	bs.lk.Lock()
	defer bs.lk.Unlock()

	bw, ok := bs.blockWaits[blk.Cid()]
	if ok {
		bw <- blk
		delete(bs.blockWaits, blk.Cid())
		return nil
	}

	bs.otherBlocks[blk.Cid()] = blk.(*block.BasicBlock)
	return nil
}

func (bs *waitingBlockstore) Complete() {
	bs.lk.Lock()
	defer bs.lk.Unlock()
	bs.streamComplete = true
	for _, ch := range bs.blockWaits {
		close(ch)
	}
}

func StreamRepoRecords(ctx context.Context, r io.Reader, prefix string, cb func(k string, c cid.Cid, v []byte) error) error {
	ctx, span := otel.Tracer("repo").Start(ctx, "RepoStream")
	defer span.End()

	br, root, err := carutil.NewReader(bufio.NewReader(r))
	if err != nil {
		return fmt.Errorf("opening CAR block reader: %w", err)
	}

	bs := newWaitingBlockstore()
	cst := util.CborStore(bs)

	var wg sync.WaitGroup
	wg.Add(1)
	var walkErr error
	go func() {
		defer wg.Done()

		var sc SignedCommit
		if err := cst.Get(ctx, root, &sc); err != nil {
			walkErr = fmt.Errorf("loading root from blockstore: %w", err)
			return
		}

		if sc.Version != ATP_REPO_VERSION && sc.Version != ATP_REPO_VERSION_2 {
			walkErr = fmt.Errorf("unsupported repo version: %d", sc.Version)
			return
		}
		// TODO: verify that signature

		t := mst.LoadMST(cst, sc.Data)

		if err := t.WalkLeavesFrom(ctx, prefix, func(k string, val cid.Cid) error {
			blk, err := bs.Get(ctx, val)
			if err != nil {
				slog.Error("failed to get record from tree", "key", k, "cid", val, "error", err)
				return nil
			}

			return cb(k, val, blk.RawData())
		}); err != nil {
			walkErr = fmt.Errorf("failed to walk mst: %w", err)
		}
	}()

	for {
		blk, err := br.NextBlock(repoBlockBufferPool, repoBlockBufferSize)
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("reading block from CAR: %w", err)
		}

		if err := bs.Put(ctx, blk); err != nil {
			return fmt.Errorf("copying block to store: %w", err)
		}
	}

	bs.Complete()

	wg.Wait()

	return walkErr
}
