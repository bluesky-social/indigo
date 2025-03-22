package repo

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/bluesky-social/indigo/mst"
	"github.com/bluesky-social/indigo/repo/carutil"
	"github.com/bluesky-social/indigo/util"
	block "github.com/ipfs/go-block-format"
	cid "github.com/ipfs/go-cid"
	"go.opentelemetry.io/otel"
)

type readStreamBlockstore struct {
	otherBlocks    map[cid.Cid]block.Block
	streamComplete bool

	r *carutil.Reader

	lastBlockRead block.Block
}

func newStreamingBlockstore(r *carutil.Reader) *readStreamBlockstore {
	return &readStreamBlockstore{
		otherBlocks: make(map[cid.Cid]block.Block),
		r:           r,
	}
}

func (bs *readStreamBlockstore) readUntilBlock(ctx context.Context, cc cid.Cid) (block.Block, error) {
	for {
		blk, err := bs.r.NextBlock(repoBlockBufferPool, repoBlockBufferSize)
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("reading block from CAR: %w", err)
		}

		if blk.Cid() == cc {
			return blk, nil
		}

		bs.otherBlocks[blk.Cid()] = blk
	}

	bs.streamComplete = true

	return nil, io.EOF
}

func (bs *readStreamBlockstore) Get(ctx context.Context, cc cid.Cid) (block.Block, error) {
	if bs.lastBlockRead != nil {
		freeRepoBlock(bs.lastBlockRead)
		bs.lastBlockRead = nil
	}

	if blk, ok := bs.otherBlocks[cc]; ok {
		delete(bs.otherBlocks, cc)
		bs.lastBlockRead = blk
		return blk, nil
	}

	if bs.streamComplete {
		return nil, ErrMissingBlock
	}

	blk, err := bs.readUntilBlock(ctx, cc)
	if err != nil {
		return nil, err
	}

	bs.lastBlockRead = blk
	return blk, nil
}

var ErrMissingBlock = fmt.Errorf("block was missing from archive")

func (bs *readStreamBlockstore) Put(ctx context.Context, blk block.Block) error {
	return fmt.Errorf("put is not needed")
}

func StreamRepoRecords(ctx context.Context, r io.Reader, prefix string, cb func(k string, c cid.Cid, v []byte) error) error {
	ctx, span := otel.Tracer("repo").Start(ctx, "RepoStream")
	defer span.End()

	br, root, err := carutil.NewReader(bufio.NewReader(r))
	if err != nil {
		return fmt.Errorf("opening CAR block reader: %w", err)
	}

	bs := newStreamingBlockstore(br)

	cst := util.CborStore(bs)

	var sc SignedCommit
	if err := cst.Get(ctx, root, &sc); err != nil {
		return fmt.Errorf("loading root from blockstore: %w", err)
	}

	if sc.Version != ATP_REPO_VERSION && sc.Version != ATP_REPO_VERSION_2 {
		return fmt.Errorf("unsupported repo version: %d", sc.Version)
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
		return fmt.Errorf("failed to walk mst: %w", err)
	}

	return nil
}
