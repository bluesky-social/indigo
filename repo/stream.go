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

const bufPoolBlockSize = 1024

var smallBlockPool = &sync.Pool{
	New: func() any {
		return make([]byte, bufPoolBlockSize)
	},
}

type readStreamBlockstore struct {
	strict         bool // expect + require every get to be the next block in the stream
	otherBlocks    map[cid.Cid]*carutil.BasicBlock
	streamComplete bool

	r *carutil.Reader
}

func newStreamingBlockstore(r *carutil.Reader, strict bool) *readStreamBlockstore {
	return &readStreamBlockstore{
		strict:      strict,
		otherBlocks: make(map[cid.Cid]*carutil.BasicBlock, 20),
		r:           r,
	}
}

func (bs *readStreamBlockstore) readUntilBlock(ctx context.Context, cc cid.Cid) (*carutil.BasicBlock, error) {
	for {
		buf := smallBlockPool.Get().([]byte)
		blk, used, err := bs.r.NextBlockBuf(buf)
		if err != nil {
			smallBlockPool.Put(buf)
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("reading block from CAR: %w", err)
		}

		if !used {
			smallBlockPool.Put(buf)
		}

		if blk.Cid() == cc {
			return blk, nil
		}

		if bs.strict {
			return nil, fmt.Errorf("%w: expected %s, got %s", ErrUnexpectedBlock, cc, blk.Cid())
		}

		bs.otherBlocks[blk.Cid()] = blk
	}

	bs.streamComplete = true

	return nil, io.EOF
}

func (bs *readStreamBlockstore) Get(ctx context.Context, cc cid.Cid) (block.Block, error) {
	return bs.get(ctx, cc)
}

func (bs *readStreamBlockstore) get(ctx context.Context, cc cid.Cid) (*carutil.BasicBlock, error) {
	if blk, ok := bs.otherBlocks[cc]; ok {
		delete(bs.otherBlocks, cc)
		return blk, nil
	}

	if bs.streamComplete {
		return nil, ErrMissingBlock
	}

	blk, err := bs.readUntilBlock(ctx, cc)
	if err != nil {
		return nil, err
	}

	return blk, nil
}

func (bs *readStreamBlockstore) View(cc cid.Cid, cb func([]byte) error) error {
	blk, err := bs.get(context.TODO(), cc)
	if err != nil {
		return err
	}

	if err := cb(blk.RawData()); err != nil {
		return err
	}

	if len(blk.BaseBuffer()) == bufPoolBlockSize {
		smallBlockPool.Put(blk.BaseBuffer())
	}

	return nil
}

var ErrMissingBlock = fmt.Errorf("block was missing from archive")
var ErrUnexpectedBlock = fmt.Errorf("next block in CAR was not the expected one")

func (bs *readStreamBlockstore) Put(ctx context.Context, blk block.Block) error {
	return fmt.Errorf("put is not needed")
}

// StreamRepoRecords parses a CAR-encoded repository from the given reader and
// iterates over its records.
//
// The prefix parameter specifies the starting point for iteration. Records are
// visited in lexicographic order starting from the first key >= prefix. If
// prefix exactly matches a full record path, that record will be included.
// Note that this does not filter to only keys with that prefix; all records
// from that point onward are visited.
//
// The onCommit callback receives the signed commit after it is loaded. It may
// be nil, in which case it is ignored. The callback can be used to extract
// the revision string (sc.Rev), or in the future to verify the commit
// signature. If the callback returns an error, StreamRepoRecords returns
// immediately with that error.
//
// The cb parameter is the visitor function called for each record with the
// record's key, CID, and raw data. If cb returns an error, the error is logged
// and iteration continues; errors from cb do not stop the walk and are not
// returned from this function. To signal intentional early termination, cb can
// return ErrDoneIterating, which is handled silently without logging.
func StreamRepoRecords(ctx context.Context, r io.Reader, prefix string, onCommit func(*SignedCommit) error, cb func(k string, c cid.Cid, v []byte) error) error {
	return streamRepoRecords(ctx, r, prefix, false, onCommit, cb)
}

// StreamRepoRecordsStrict is like StreamRepoRecords but requires that blocks
// in the CAR file appear in the exact order they are accessed during repo
// traversal, i.e. sync1.1 order. Out-of-order blocks produce an
// ErrUnexpectedBlock error instead of being buffered.
func StreamRepoRecordsStrict(ctx context.Context, r io.Reader, prefix string, onCommit func(*SignedCommit) error, cb func(k string, c cid.Cid, v []byte) error) error {
	return streamRepoRecords(ctx, r, prefix, true, onCommit, cb)
}

func streamRepoRecords(ctx context.Context, r io.Reader, prefix string, strict bool, onCommit func(*SignedCommit) error, cb func(k string, c cid.Cid, v []byte) error) error {
	ctx, span := otel.Tracer("repo").Start(ctx, "RepoStream")
	defer span.End()

	if onCommit == nil {
		onCommit = func(*SignedCommit) error { return nil }
	}

	br, root, err := carutil.NewReader(bufio.NewReader(r))
	if err != nil {
		return fmt.Errorf("opening CAR block reader: %w", err)
	}

	bs := newStreamingBlockstore(br, strict)

	cst := util.CborStore(bs)

	var sc SignedCommit
	if err := cst.Get(ctx, root, &sc); err != nil {
		return fmt.Errorf("loading root (%s) from blockstore (other blocks: %d): %w", root, len(bs.otherBlocks), err)
	}

	if sc.Version != ATP_REPO_VERSION && sc.Version != ATP_REPO_VERSION_2 {
		return fmt.Errorf("unsupported repo version: %d", sc.Version)
	}

	if err := onCommit(&sc); err != nil {
		return fmt.Errorf("commit callback: %w", err)
	}

	t := mst.LoadMST(cst, sc.Data)

	if err := t.WalkLeavesFromNocache(ctx, prefix, func(k string, val cid.Cid) error {
		if err := bs.View(val, func(data []byte) error {
			return cb(k, val, data)
		}); err != nil {
			if err == ErrDoneIterating {
				return nil
			}
			if strict {
				return err
			}
			slog.Error("failed to get record from tree", "key", k, "cid", val, "error", err)
			return nil
		}

		return nil
	}); err != nil {
		return fmt.Errorf("failed to walk mst: %w", err)
	}

	return nil
}
