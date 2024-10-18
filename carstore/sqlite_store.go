package carstore

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"github.com/bluesky-social/indigo/models"
	blockformat "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	"github.com/ipld/go-car"
	_ "github.com/mattn/go-sqlite3"
	"go.opentelemetry.io/otel"
	"io"
)

//var log = logging.Logger("sqstore")

type SQLiteStore struct {
	db *sql.DB

	lastShardCache lastShardCache
}

func (sqs *SQLiteStore) Open(path string) error {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return err
	}
	sqs.db = db
	sqs.lastShardCache.source = sqs
	return nil
}

// writeNewShard needed for DeltaSession.CloseWithRoot
func (sqs *SQLiteStore) writeNewShard(ctx context.Context, root cid.Cid, rev string, user models.Uid, seq int, blks map[cid.Cid]blockformat.Block, rmcids map[cid.Cid]bool) ([]byte, error) {
	//TODO implement me
	panic("implement me")
}

// GetLastShard nedeed for NewDeltaSession indirectly through lastShardCache
func (sqs *SQLiteStore) GetLastShard(ctx context.Context, uid models.Uid) (*CarShard, error) {
	//TODO implement me
	panic("implement me")
}

func (sqs *SQLiteStore) CompactUserShards(ctx context.Context, user models.Uid, skipBigShards bool) (*CompactionStats, error) {
	//TODO remove from CarStore interface
	panic("implement me")
}

func (sqs *SQLiteStore) GetCompactionTargets(ctx context.Context, shardCount int) ([]CompactionTarget, error) {
	//TODO remove from CarStore interface
	return nil, nil
}

func (sqs *SQLiteStore) GetUserRepoHead(ctx context.Context, user models.Uid) (cid.Cid, error) {
	//TODO implement me
	panic("implement me")
}

func (sqs *SQLiteStore) GetUserRepoRev(ctx context.Context, user models.Uid) (string, error) {
	//TODO implement me
	panic("implement me")
}

func (sqs *SQLiteStore) ImportSlice(ctx context.Context, uid models.Uid, since *string, carslice []byte) (cid.Cid, *DeltaSession, error) {
	// TODO: same as FileCarStore, re-unify
	ctx, span := otel.Tracer("carstore").Start(ctx, "ImportSlice")
	defer span.End()

	carr, err := car.NewCarReader(bytes.NewReader(carslice))
	if err != nil {
		return cid.Undef, nil, err
	}

	if len(carr.Header.Roots) != 1 {
		return cid.Undef, nil, fmt.Errorf("invalid car file, header must have a single root (has %d)", len(carr.Header.Roots))
	}

	ds, err := sqs.NewDeltaSession(ctx, uid, since)
	if err != nil {
		return cid.Undef, nil, fmt.Errorf("new delta session failed: %w", err)
	}

	var cids []cid.Cid
	for {
		blk, err := carr.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return cid.Undef, nil, err
		}

		cids = append(cids, blk.Cid())

		if err := ds.Put(ctx, blk); err != nil {
			return cid.Undef, nil, err
		}
	}

	return carr.Header.Roots[0], ds, nil
}

func (sqs *SQLiteStore) NewDeltaSession(ctx context.Context, user models.Uid, since *string) (*DeltaSession, error) {
	ctx, span := otel.Tracer("carstore").Start(ctx, "NewSession")
	defer span.End()

	// TODO: ensure that we don't write updates on top of the wrong head
	// this needs to be a compare and swap type operation
	lastShard, err := sqs.lastShardCache.get(ctx, user)
	if err != nil {
		return nil, err
	}

	if since != nil && *since != lastShard.Rev {
		return nil, fmt.Errorf("revision mismatch: %s != %s: %w", *since, lastShard.Rev, ErrRepoBaseMismatch)
	}

	return &DeltaSession{
		fresh: blockstore.NewBlockstore(datastore.NewMapDatastore()),
		blks:  make(map[cid.Cid]blockformat.Block),
		base: &userView{
			user:     user,
			cs:       sqs,
			prefetch: true,
			cache:    make(map[cid.Cid]blockformat.Block),
		},
		user:    user,
		baseCid: lastShard.Root.CID,
		cs:      sqs,
		seq:     lastShard.Seq + 1,
		lastRev: lastShard.Rev,
	}, nil
}

func (sqs *SQLiteStore) ReadOnlySession(user models.Uid) (*DeltaSession, error) {
	//TODO implement me
	panic("implement me")
}

func (sqs *SQLiteStore) ReadUserCar(ctx context.Context, user models.Uid, sinceRev string, incremental bool, w io.Writer) error {
	//TODO implement me
	panic("implement me")
}

func (sqs *SQLiteStore) Stat(ctx context.Context, usr models.Uid) ([]UserStat, error) {
	//TODO implement me
	panic("implement me")
}

func (sqs *SQLiteStore) WipeUserData(ctx context.Context, user models.Uid) error {
	//TODO implement me
	panic("implement me")
}

// HasUidCid needed for NewDeltaSession userView
func (sqs *SQLiteStore) HasUidCid(ctx context.Context, user models.Uid, k cid.Cid) (bool, error) {
	//TODO implement me
	panic("implement me")
}

// LookupBlockRef needed for NewDeltaSession userView
func (sqs *SQLiteStore) LookupBlockRef(ctx context.Context, k cid.Cid) (path string, offset int64, user models.Uid, err error) {
	//TODO implement me
	panic("implement me")
}

func (sqs *SQLiteStore) CarStore() CarStore {
	return sqs
}

func (sqs *SQLiteStore) Close() {
	sqs.db.Close()
}
