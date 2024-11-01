package carstore

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/bluesky-social/indigo/models"
	blockformat "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	"github.com/ipfs/go-libipfs/blocks"
	"github.com/ipld/go-car"
	_ "github.com/mattn/go-sqlite3"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.opentelemetry.io/otel"
	"io"
	"log/slog"
)

//var log = logging.Logger("sqstore")

type SQLiteStore struct {
	db *sql.DB

	lastShardCache lastShardCache
}

func (sqs *SQLiteStore) Open(path string) error {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return fmt.Errorf("%s: sqlite could not open, %w", path, err)
	}
	_, err = db.Exec("CREATE TABLE IF NOT EXISTS blocks (uid int, cid blob, rev varchar, root varchar, block blob, PRIMARY KEY(uid,cid))")
	if err != nil {
		return fmt.Errorf("%s: create table blocks..., %w", path, err)
	}
	sqs.db = db
	sqs.lastShardCache.source = sqs
	sqs.lastShardCache.Init()
	return nil
}

// writeNewShard needed for DeltaSession.CloseWithRoot
func (sqs *SQLiteStore) writeNewShard(ctx context.Context, root cid.Cid, rev string, user models.Uid, seq int, blks map[cid.Cid]blockformat.Block, rmcids map[cid.Cid]bool) ([]byte, error) {
	sqWriteNewShard.Inc()
	// TODO: trace span here
	// this is "write many blocks", "write one block" is above in putBlock(). keep them in sync.
	buf := new(bytes.Buffer)
	hnw, err := WriteCarHeader(buf, root)
	if err != nil {
		return nil, fmt.Errorf("failed to write car header: %w", err)
	}
	offset := hnw

	insertStatement, err := sqs.db.PrepareContext(ctx, "INSERT INTO blocks (uid, cid, rev, root, block) VALUES (?, ?, ?, ?, ?)")
	if err != nil {
		return nil, fmt.Errorf("bad block insert sql, %w", err)
	}
	defer insertStatement.Close()

	dbroot := models.DbCID{root}

	for bcid, block := range blks {
		// build shard for output firehose
		nw, err := LdWrite(buf, bcid.Bytes(), block.RawData())
		if err != nil {
			return nil, fmt.Errorf("failed to write block: %w", err)
		}
		offset += nw

		// TODO: better databases have an insert-many option for a prepared statement
		dbcid := models.DbCID{CID: bcid}
		_, err = insertStatement.ExecContext(ctx, user, dbcid, rev, dbroot, block.RawData())
		if err != nil {
			return nil, fmt.Errorf("(uid,cid) block store failed, %w", err)
		}
	}

	shard := CarShard{
		Root:      models.DbCID{CID: root},
		DataStart: hnw,
		Seq:       seq,
		Usr:       user,
		Rev:       rev,
	}

	sqs.lastShardCache.put(&shard)

	return buf.Bytes(), nil
}

var ErrNothingThere = errors.New("nothing to read)")

// GetLastShard nedeed for NewDeltaSession indirectly through lastShardCache
// What we actually seem to need from this: last {Rev, Root.CID}
func (sqs *SQLiteStore) GetLastShard(ctx context.Context, uid models.Uid) (*CarShard, error) {
	sqGetLastShard.Inc()
	qstmt, err := sqs.db.PrepareContext(ctx, "SELECT rev, root FROM blocks WHERE uid = ? ORDER BY rev DESC LIMIT 1")
	if err != nil {
		return nil, fmt.Errorf("bad last shard sql, %w", err)
	}
	rows, err := qstmt.QueryContext(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("last shard err, %w", err)
	}
	if rows.Next() {
		var rev string
		var rootb models.DbCID
		err = rows.Scan(&rev, &rootb)
		if err != nil {
			return nil, fmt.Errorf("last shard bad scan, %w", err)
		}
		return &CarShard{
			Root: rootb,
			Rev:  rev,
		}, nil
	}
	return nil, nil
}

func (sqs *SQLiteStore) CompactUserShards(ctx context.Context, user models.Uid, skipBigShards bool) (*CompactionStats, error) {
	slog.Warn("TODO: don't call compaction")
	return nil, nil
}

func (sqs *SQLiteStore) GetCompactionTargets(ctx context.Context, shardCount int) ([]CompactionTarget, error) {
	slog.Warn("TODO: don't call compaction targets")
	return nil, nil
}

func (sqs *SQLiteStore) GetUserRepoHead(ctx context.Context, user models.Uid) (cid.Cid, error) {
	// TODO: same as FileCarStore; re-unify
	lastShard, err := sqs.lastShardCache.get(ctx, user)
	if err != nil {
		return cid.Undef, err
	}
	if lastShard.ID == 0 {
		return cid.Undef, nil
	}

	return lastShard.Root.CID, nil
}

func (sqs *SQLiteStore) GetUserRepoRev(ctx context.Context, user models.Uid) (string, error) {
	// TODO: same as FileCarStore; re-unify
	lastShard, err := sqs.lastShardCache.get(ctx, user)
	if err != nil {
		return "", err
	}
	if lastShard.ID == 0 {
		return "", nil
	}

	return lastShard.Rev, nil
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

var zeroShard CarShard

func (sqs *SQLiteStore) NewDeltaSession(ctx context.Context, user models.Uid, since *string) (*DeltaSession, error) {
	ctx, span := otel.Tracer("carstore").Start(ctx, "NewSession")
	defer span.End()

	// TODO: ensure that we don't write updates on top of the wrong head
	// this needs to be a compare and swap type operation
	lastShard, err := sqs.lastShardCache.get(ctx, user)
	if err != nil {
		return nil, fmt.Errorf("NewDeltaSession, lsc, %w", err)
	}

	if lastShard == nil {
		lastShard = &zeroShard
	}

	if since != nil && *since != lastShard.Rev {
		return nil, fmt.Errorf("revision mismatch: %s != %s: %w", *since, lastShard.Rev, ErrRepoBaseMismatch)
	}

	return &DeltaSession{
		fresh: blockstore.NewBlockstore(datastore.NewMapDatastore()),
		blks:  make(map[cid.Cid]blockformat.Block),
		base: &sqliteUserView{
			uid: user,
			sqs: sqs,
		},
		user:    user,
		baseCid: lastShard.Root.CID,
		cs:      sqs,
		seq:     lastShard.Seq + 1,
		lastRev: lastShard.Rev,
	}, nil
}

func (sqs *SQLiteStore) ReadOnlySession(user models.Uid) (*DeltaSession, error) {
	return &DeltaSession{
		base: &sqliteUserView{
			uid: user,
			sqs: sqs,
		},
		readonly: true,
		user:     user,
		cs:       sqs,
	}, nil
}

// ReadUserCar
// incremental is only ever called true
func (sqs *SQLiteStore) ReadUserCar(ctx context.Context, user models.Uid, sinceRev string, incremental bool, w io.Writer) error {
	//TODO implement me
	// TODO: get help understanding what PDS does for this
	panic("implement me")
}

// Stat is only used in a debugging admin handler
// don't bother implementing it (for now?)
func (sqs *SQLiteStore) Stat(ctx context.Context, usr models.Uid) ([]UserStat, error) {
	slog.Warn("Stat debugging method not implemented for sqlite store")
	return nil, nil
}

func (sqs *SQLiteStore) WipeUserData(ctx context.Context, user models.Uid) error {
	deleteResult, err := sqs.db.ExecContext(ctx, "DELETE FROM blocks WHERE uid = ?", user)
	nrows, ierr := deleteResult.RowsAffected()
	if ierr == nil {
		sqRowsDeleted.Add(float64(nrows))
	}
	if err == nil {
		err = ierr
	}
	return err
}

// HasUidCid needed for NewDeltaSession userView
func (sqs *SQLiteStore) HasUidCid(ctx context.Context, user models.Uid, bcid cid.Cid) (bool, error) {
	// TODO: this is pretty cacheable? invalidate (uid,*) on WipeUserData
	sqHas.Inc()
	qstmt, err := sqs.db.PrepareContext(ctx, "SELECT rev, root FROM blocks WHERE uid = ? AND cid = ? LIMIT 1")
	if err != nil {
		return false, fmt.Errorf("hasUC sql, %w", err)
	}
	defer qstmt.Close()
	rows, err := qstmt.QueryContext(ctx, user, models.DbCID{bcid})
	if err != nil {
		return false, fmt.Errorf("hasUC err, %w", err)
	}
	if rows.Next() {
		var rev string
		var rootb models.DbCID
		err = rows.Scan(&rev, &rootb)
		if err != nil {
			return false, fmt.Errorf("hasUC bad scan, %w", err)
		}
		return true, nil
	}
	return false, nil
}

func (sqs *SQLiteStore) CarStore() CarStore {
	return sqs
}

func (sqs *SQLiteStore) Close() error {
	return sqs.db.Close()
}

func (sqs *SQLiteStore) getBlock(ctx context.Context, user models.Uid, bcid cid.Cid) (blockformat.Block, error) {
	// TODO: this is pretty cacheable? invalidate (uid,*) on WipeUserData
	sqGetBlock.Inc()
	qstmt, err := sqs.db.PrepareContext(ctx, "SELECT block FROM blocks WHERE uid = ? AND cid = ? LIMIT 1")
	defer qstmt.Close()
	if err != nil {
		return nil, fmt.Errorf("hasUC sql, %w", err)
	}
	rows, err := qstmt.QueryContext(ctx, user, models.DbCID{bcid})
	if err != nil {
		return nil, fmt.Errorf("hasUC err, %w", err)
	}
	if rows.Next() {
		//var rev string
		//var rootb models.DbCID
		var blockb []byte
		err = rows.Scan(&blockb)
		if err != nil {
			return nil, fmt.Errorf("hasUC bad scan, %w", err)
		}
		return blocks.NewBlock(blockb), nil
	}
	return nil, ErrNothingThere
}

func (sqs *SQLiteStore) getBlockSize(ctx context.Context, user models.Uid, bcid cid.Cid) (int64, error) {
	// TODO: this is pretty cacheable? invalidate (uid,*) on WipeUserData
	sqGetBlockSize.Inc()
	qstmt, err := sqs.db.PrepareContext(ctx, "SELECT length(block) FROM blocks WHERE uid = ? AND cid = ? LIMIT 1")
	if err != nil {
		return 0, fmt.Errorf("hasUC sql, %w", err)
	}
	defer qstmt.Close()
	rows, err := qstmt.QueryContext(ctx, user, models.DbCID{bcid})
	if err != nil {
		return 0, fmt.Errorf("hasUC err, %w", err)
	}
	if rows.Next() {
		var out int64
		err = rows.Scan(&out)
		if err != nil {
			return 0, fmt.Errorf("hasUC bad scan, %w", err)
		}
		return out, nil
	}
	return 0, nil
}

type sqliteUserView struct {
	sqs *SQLiteStore
	uid models.Uid
}

func (s sqliteUserView) Has(ctx context.Context, c cid.Cid) (bool, error) {
	// TODO: cache block metadata?
	return s.sqs.HasUidCid(ctx, s.uid, c)
}

func (s sqliteUserView) Get(ctx context.Context, c cid.Cid) (blockformat.Block, error) {
	// TODO: cache blocks?
	return s.sqs.getBlock(ctx, s.uid, c)
}

func (s sqliteUserView) GetSize(ctx context.Context, c cid.Cid) (int, error) {
	// TODO: cache block metadata?
	bigsize, err := s.sqs.getBlockSize(ctx, s.uid, c)
	return int(bigsize), err
}

// ensure we implement the interface
var _ minBlockstore = (*sqliteUserView)(nil)

var sqRowsDeleted = promauto.NewCounter(prometheus.CounterOpts{
	Name: "bgs_sq_rows_deleted",
	Help: "User rows deleted in sqlite backend",
})

var sqGetBlock = promauto.NewCounter(prometheus.CounterOpts{
	Name: "bgs_sq_get_block",
	Help: "get block sqlite backend",
})

var sqGetBlockSize = promauto.NewCounter(prometheus.CounterOpts{
	Name: "bgs_sq_get_block_size",
	Help: "get block size sqlite backend",
})

var sqHas = promauto.NewCounter(prometheus.CounterOpts{
	Name: "bgs_sq_has",
	Help: "check block presence sqlite backend",
})

var sqGetLastShard = promauto.NewCounter(prometheus.CounterOpts{
	Name: "bgs_sq_get_last_shard",
	Help: "get last shard sqlite backend",
})

var sqWriteNewShard = promauto.NewCounter(prometheus.CounterOpts{
	Name: "bgs_sq_write_shard",
	Help: "write shard blocks sqlite backend",
})
