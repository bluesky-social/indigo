//go:build scylla

package carstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/bluesky-social/indigo/models"
	"github.com/gocql/gocql"
	blockformat "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-libipfs/blocks"
	"github.com/ipld/go-car"
	_ "github.com/mattn/go-sqlite3"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"time"
)

type ScyllaStore struct {
	WriteSession *gocql.Session
	ReadSession  *gocql.Session

	// scylla servers
	scyllaAddrs []string
	// scylla namespace where we find our table
	keyspace string

	log *slog.Logger

	lastShardCache lastShardCache
}

func NewScyllaStore(addrs []string, keyspace string) (*ScyllaStore, error) {
	out := new(ScyllaStore)
	out.scyllaAddrs = addrs
	out.keyspace = keyspace
	err := out.Open()
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (sqs *ScyllaStore) Open() error {
	if sqs.log == nil {
		sqs.log = slog.Default()
	}
	sqs.log.Debug("scylla connect", "addrs", sqs.scyllaAddrs)
	var err error

	//
	// Write session
	//
	var writeSession *gocql.Session
	for retry := 0; ; retry++ {
		writeCluster := gocql.NewCluster(sqs.scyllaAddrs...)
		writeCluster.Keyspace = sqs.keyspace
		// Default port, the client should automatically upgrade to shard-aware port
		writeCluster.Port = 9042
		writeCluster.Consistency = gocql.Quorum
		writeCluster.RetryPolicy = &ExponentialBackoffRetryPolicy{NumRetries: 10, Min: 100 * time.Millisecond, Max: 10 * time.Second}
		writeCluster.PoolConfig.HostSelectionPolicy = gocql.TokenAwareHostPolicy(gocql.RoundRobinHostPolicy())
		writeSession, err = writeCluster.CreateSession()
		if err != nil {
			if retry > 200 {
				return fmt.Errorf("failed to connect read session too many times: %w", err)
			}
			sqs.log.Error("failed to connect to ScyllaDB Read Session, retrying in 1s", "retry", retry, "err", err)
			time.Sleep(delayForAttempt(retry))
			continue
		}
		break
	}

	//
	// Read session
	//
	var readSession *gocql.Session
	for retry := 0; ; retry++ {
		readCluster := gocql.NewCluster(sqs.scyllaAddrs...)
		readCluster.Keyspace = sqs.keyspace
		// Default port, the client should automatically upgrade to shard-aware port
		readCluster.Port = 9042
		readCluster.RetryPolicy = &ExponentialBackoffRetryPolicy{NumRetries: 5, Min: 10 * time.Millisecond, Max: 1 * time.Second}
		readCluster.Consistency = gocql.One
		readCluster.PoolConfig.HostSelectionPolicy = gocql.TokenAwareHostPolicy(gocql.RoundRobinHostPolicy())
		readSession, err = readCluster.CreateSession()
		if err != nil {
			if retry > 200 {
				return fmt.Errorf("failed to connect read session too many times: %w", err)
			}
			sqs.log.Error("failed to connect to ScyllaDB Read Session, retrying in 1s", "retry", retry, "err", err)
			time.Sleep(delayForAttempt(retry))
			continue
		}
		break
	}

	sqs.WriteSession = writeSession
	sqs.ReadSession = readSession

	err = sqs.createTables()
	if err != nil {
		return fmt.Errorf("scylla could not create tables, %w", err)
	}
	sqs.lastShardCache.source = sqs
	sqs.lastShardCache.Init()
	return nil
}

var createTableTexts = []string{
	`CREATE TABLE IF NOT EXISTS blocks (uid bigint, cid blob, rev varchar, root blob, block blob, PRIMARY KEY((uid,cid)))`,
	// This is the INDEX I wish we could use, but scylla can't do it so we MATERIALIZED VIEW instead
	//`CREATE INDEX IF NOT EXISTS block_by_rev ON blocks (uid, rev)`,
	`CREATE MATERIALIZED VIEW IF NOT EXISTS blocks_by_uidrev
AS SELECT uid, rev, cid, root
FROM blocks
WHERE uid IS NOT NULL AND rev IS NOT NULL AND cid IS NOT NULL
PRIMARY KEY ((uid), rev, cid) WITH CLUSTERING ORDER BY (rev DESC)`,
}

func (sqs *ScyllaStore) createTables() error {
	for i, text := range createTableTexts {
		err := sqs.WriteSession.Query(text).Exec()
		if err != nil {
			return fmt.Errorf("scylla create table statement [%d] %v: %w", i, text, err)
		}
	}
	return nil
}

// writeNewShard needed for DeltaSession.CloseWithRoot
func (sqs *ScyllaStore) writeNewShard(ctx context.Context, root cid.Cid, rev string, user models.Uid, seq int, blks map[cid.Cid]blockformat.Block, rmcids map[cid.Cid]bool) ([]byte, error) {
	scWriteNewShard.Inc()
	sqs.log.Debug("write shard", "uid", user, "root", root, "rev", rev, "nblocks", len(blks))
	start := time.Now()
	ctx, span := otel.Tracer("carstore").Start(ctx, "writeNewShard")
	defer span.End()
	buf := new(bytes.Buffer)
	hnw, err := WriteCarHeader(buf, root)
	if err != nil {
		return nil, fmt.Errorf("failed to write car header: %w", err)
	}
	offset := hnw

	dbroot := root.Bytes()

	span.SetAttributes(attribute.Int("blocks", len(blks)))

	for bcid, block := range blks {
		// build shard for output firehose
		nw, err := LdWrite(buf, bcid.Bytes(), block.RawData())
		if err != nil {
			return nil, fmt.Errorf("failed to write block: %w", err)
		}
		offset += nw

		// TODO: scylla BATCH doesn't apply if the batch crosses partition keys; BUT, we may be able to send many blocks concurrently?
		dbcid := bcid.Bytes()
		blockbytes := block.RawData()
		// we're relying on cql auto-prepare, no 'PreparedStatement'
		err = sqs.WriteSession.Query(
			`INSERT INTO blocks (uid, cid, rev, root, block) VALUES (?, ?, ?, ?, ?)`,
			user, dbcid, rev, dbroot, blockbytes,
		).Idempotent(true).Exec()
		if err != nil {
			return nil, fmt.Errorf("(uid,cid) block store failed, %w", err)
		}
		sqs.log.Debug("put block", "uid", user, "cid", bcid, "size", len(blockbytes))
	}

	shard := CarShard{
		Root:      models.DbCID{CID: root},
		DataStart: hnw,
		Seq:       seq,
		Usr:       user,
		Rev:       rev,
	}

	sqs.lastShardCache.put(&shard)

	dt := time.Since(start).Seconds()
	scWriteTimes.Observe(dt)
	return buf.Bytes(), nil
}

// GetLastShard nedeed for NewDeltaSession indirectly through lastShardCache
// What we actually seem to need from this: last {Rev, Root.CID}
func (sqs *ScyllaStore) GetLastShard(ctx context.Context, uid models.Uid) (*CarShard, error) {
	scGetLastShard.Inc()
	var rev string
	var rootb []byte
	err := sqs.ReadSession.Query(`SELECT rev, root FROM blocks_by_uidrev WHERE uid = ? ORDER BY rev DESC LIMIT 1`, uid).Scan(&rev, &rootb)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("last shard err, %w", err)
	}
	xcid, cidErr := cid.Cast(rootb)
	if cidErr != nil {
		return nil, fmt.Errorf("last shard bad cid, %w", cidErr)
	}
	return &CarShard{
		Root: models.DbCID{CID: xcid},
		Rev:  rev,
	}, nil
}

func (sqs *ScyllaStore) CompactUserShards(ctx context.Context, user models.Uid, skipBigShards bool) (*CompactionStats, error) {
	sqs.log.Warn("TODO: don't call compaction")
	return nil, nil
}

func (sqs *ScyllaStore) GetCompactionTargets(ctx context.Context, shardCount int) ([]CompactionTarget, error) {
	sqs.log.Warn("TODO: don't call compaction targets")
	return nil, nil
}

func (sqs *ScyllaStore) GetUserRepoHead(ctx context.Context, user models.Uid) (cid.Cid, error) {
	// TODO: same as FileCarStore; re-unify
	lastShard, err := sqs.lastShardCache.get(ctx, user)
	if err != nil {
		return cid.Undef, err
	}
	if lastShard == nil {
		return cid.Undef, nil
	}
	if lastShard.ID == 0 {
		return cid.Undef, nil
	}

	return lastShard.Root.CID, nil
}

func (sqs *ScyllaStore) GetUserRepoRev(ctx context.Context, user models.Uid) (string, error) {
	// TODO: same as FileCarStore; re-unify
	lastShard, err := sqs.lastShardCache.get(ctx, user)
	if err != nil {
		return "", err
	}
	if lastShard == nil {
		return "", nil
	}
	if lastShard.ID == 0 {
		return "", nil
	}

	return lastShard.Rev, nil
}

func (sqs *ScyllaStore) ImportSlice(ctx context.Context, uid models.Uid, since *string, carslice []byte) (cid.Cid, *DeltaSession, error) {
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

func (sqs *ScyllaStore) NewDeltaSession(ctx context.Context, user models.Uid, since *string) (*DeltaSession, error) {
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
		blks: make(map[cid.Cid]blockformat.Block),
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

func (sqs *ScyllaStore) ReadOnlySession(user models.Uid) (*DeltaSession, error) {
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
func (sqs *ScyllaStore) ReadUserCar(ctx context.Context, user models.Uid, sinceRev string, incremental bool, shardOut io.Writer) error {
	scGetCar.Inc()
	ctx, span := otel.Tracer("carstore").Start(ctx, "ReadUserCar")
	defer span.End()
	start := time.Now()

	cidchan := make(chan cid.Cid, 100)

	go func() {
		defer close(cidchan)
		cids := sqs.ReadSession.Query(`SELECT cid FROM blocks_by_uidrev WHERE uid = ? AND rev > ? ORDER BY rev DESC`, user, sinceRev).Iter()
		defer cids.Close()
		for {
			var cidb []byte
			ok := cids.Scan(&cidb)
			if !ok {
				break
			}
			xcid, cidErr := cid.Cast(cidb)
			if cidErr != nil {
				sqs.log.Warn("ReadUserCar bad cid", "err", cidErr)
				continue
			}
			cidchan <- xcid
		}
	}()
	nblocks := 0
	first := true
	for xcid := range cidchan {
		var xrev string
		var xroot []byte
		var xblock []byte
		err := sqs.ReadSession.Query("SELECT rev, root, block FROM blocks WHERE uid = ? AND cid = ? LIMIT 1", user, xcid.Bytes()).Scan(&xrev, &xroot, &xblock)
		if err != nil {
			return fmt.Errorf("rcar bad read, %w", err)
		}
		if first {
			rootCid, cidErr := cid.Cast(xroot)
			if cidErr != nil {
				return fmt.Errorf("rcar bad rootcid, %w", err)
			}
			if err := car.WriteHeader(&car.CarHeader{
				Roots:   []cid.Cid{rootCid},
				Version: 1,
			}, shardOut); err != nil {
				return fmt.Errorf("rcar bad header, %w", err)
			}
			first = false
		}
		nblocks++
		_, err = LdWrite(shardOut, xcid.Bytes(), xblock)
		if err != nil {
			return fmt.Errorf("rcar bad write, %w", err)
		}
	}
	span.SetAttributes(attribute.Int("blocks", nblocks))
	sqs.log.Debug("read car", "nblocks", nblocks, "since", sinceRev)
	scReadCarTimes.Observe(time.Since(start).Seconds())
	return nil
}

// Stat is only used in a debugging admin handler
// don't bother implementing it (for now?)
func (sqs *ScyllaStore) Stat(ctx context.Context, usr models.Uid) ([]UserStat, error) {
	sqs.log.Warn("Stat debugging method not implemented for sqlite store")
	return nil, nil
}

func (sqs *ScyllaStore) WipeUserData(ctx context.Context, user models.Uid) error {
	ctx, span := otel.Tracer("carstore").Start(ctx, "WipeUserData")
	defer span.End()

	// LOL, can't do this if primary key is (uid,cid) because that's hashed with no scan!
	//err := sqs.WriteSession.Query("DELETE FROM blocks WHERE uid = ?", user).Exec()

	cidchan := make(chan cid.Cid, 100)

	go func() {
		defer close(cidchan)
		cids := sqs.ReadSession.Query(`SELECT cid FROM blocks_by_uidrev WHERE uid = ?`, user).Iter()
		defer cids.Close()
		for {
			var cidb []byte
			ok := cids.Scan(&cidb)
			if !ok {
				break
			}
			xcid, cidErr := cid.Cast(cidb)
			if cidErr != nil {
				sqs.log.Warn("ReadUserCar bad cid", "err", cidErr)
				continue
			}
			cidchan <- xcid
		}
	}()
	nblocks := 0
	errcount := 0
	for xcid := range cidchan {
		err := sqs.ReadSession.Query("DELETE FROM blocks WHERE uid = ? AND cid = ?", user, xcid.Bytes()).Exec()
		if err != nil {
			sqs.log.Warn("ReadUserCar bad delete", "err", err)
			errcount++
			if errcount > 10 {
				return fmt.Errorf("ReadUserCar bad delete: %w", err)
			}
		}
		nblocks++
	}
	scUsersWiped.Inc()
	scBlocksDeleted.Add(float64(nblocks))
	return nil
}

// HasUidCid needed for NewDeltaSession userView
func (sqs *ScyllaStore) HasUidCid(ctx context.Context, user models.Uid, bcid cid.Cid) (bool, error) {
	// TODO: this is pretty cacheable? invalidate (uid,*) on WipeUserData
	scHas.Inc()
	var rev string
	var rootb []byte
	err := sqs.ReadSession.Query(`SELECT rev, root FROM blocks WHERE uid = ? AND cid = ? LIMIT 1`, user, bcid.Bytes()).Scan(&rev, &rootb)
	if err != nil {
		return false, fmt.Errorf("hasUC bad scan, %w", err)
	}
	return true, nil
}

func (sqs *ScyllaStore) CarStore() CarStore {
	return sqs
}

func (sqs *ScyllaStore) Close() error {
	sqs.WriteSession.Close()
	sqs.ReadSession.Close()
	return nil
}

func (sqs *ScyllaStore) getBlock(ctx context.Context, user models.Uid, bcid cid.Cid) (blockformat.Block, error) {
	// TODO: this is pretty cacheable? invalidate (uid,*) on WipeUserData
	scGetBlock.Inc()
	start := time.Now()
	var blockb []byte
	err := sqs.ReadSession.Query("SELECT block FROM blocks WHERE uid = ? AND cid = ? LIMIT 1", user, bcid.Bytes()).Scan(&blockb)
	if err != nil {
		return nil, fmt.Errorf("getb err, %w", err)
	}
	dt := time.Since(start)
	scGetTimes.Observe(dt.Seconds())
	return blocks.NewBlock(blockb), nil
}

func (sqs *ScyllaStore) getBlockSize(ctx context.Context, user models.Uid, bcid cid.Cid) (int64, error) {
	// TODO: this is pretty cacheable? invalidate (uid,*) on WipeUserData
	scGetBlockSize.Inc()
	var out int64
	err := sqs.ReadSession.Query("SELECT length(block) FROM blocks WHERE uid = ? AND cid = ? LIMIT 1", user, bcid.Bytes()).Scan(&out)
	if err != nil {
		return 0, fmt.Errorf("getbs err, %w", err)
	}
	return out, nil
}

var scUsersWiped = promauto.NewCounter(prometheus.CounterOpts{
	Name: "bgs_sc_users_wiped",
	Help: "User rows deleted in scylla backend",
})

var scBlocksDeleted = promauto.NewCounter(prometheus.CounterOpts{
	Name: "bgs_sc_blocks_deleted",
	Help: "User blocks deleted in scylla backend",
})

var scGetBlock = promauto.NewCounter(prometheus.CounterOpts{
	Name: "bgs_sc_get_block",
	Help: "get block scylla backend",
})

var scGetBlockSize = promauto.NewCounter(prometheus.CounterOpts{
	Name: "bgs_sc_get_block_size",
	Help: "get block size scylla backend",
})

var scGetCar = promauto.NewCounter(prometheus.CounterOpts{
	Name: "bgs_sc_get_car",
	Help: "get block scylla backend",
})

var scHas = promauto.NewCounter(prometheus.CounterOpts{
	Name: "bgs_sc_has",
	Help: "check block presence scylla backend",
})

var scGetLastShard = promauto.NewCounter(prometheus.CounterOpts{
	Name: "bgs_sc_get_last_shard",
	Help: "get last shard scylla backend",
})

var scWriteNewShard = promauto.NewCounter(prometheus.CounterOpts{
	Name: "bgs_sc_write_shard",
	Help: "write shard blocks scylla backend",
})

var timeBuckets []float64
var scWriteTimes prometheus.Histogram
var scGetTimes prometheus.Histogram
var scReadCarTimes prometheus.Histogram

func init() {
	timeBuckets = make([]float64, 1, 20)
	timeBuckets[0] = 0.000_0100
	i := 0
	for timeBuckets[i] < 1 && len(timeBuckets) < 20 {
		timeBuckets = append(timeBuckets, timeBuckets[i]*2)
		i++
	}
	scWriteTimes = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "bgs_sc_write_times",
		Buckets: timeBuckets,
	})
	scGetTimes = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "bgs_sc_get_times",
		Buckets: timeBuckets,
	})
	scReadCarTimes = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "bgs_sc_readcar_times",
		Buckets: timeBuckets,
	})
}

// TODO: copied from tango, re-unify?
// ExponentialBackoffRetryPolicy sleeps between attempts
type ExponentialBackoffRetryPolicy struct {
	NumRetries int
	Min, Max   time.Duration
}

func (e *ExponentialBackoffRetryPolicy) napTime(attempts int) time.Duration {
	return getExponentialTime(e.Min, e.Max, attempts)
}

func (e *ExponentialBackoffRetryPolicy) Attempt(q gocql.RetryableQuery) bool {
	if q.Attempts() > e.NumRetries {
		return false
	}
	time.Sleep(e.napTime(q.Attempts()))
	return true
}

// used to calculate exponentially growing time
func getExponentialTime(min time.Duration, max time.Duration, attempts int) time.Duration {
	if min <= 0 {
		min = 100 * time.Millisecond
	}
	if max <= 0 {
		max = 10 * time.Second
	}
	minFloat := float64(min)
	napDuration := minFloat * math.Pow(2, float64(attempts-1))
	// add some jitter
	napDuration += rand.Float64()*minFloat - (minFloat / 2)
	if napDuration > float64(max) {
		return time.Duration(max)
	}
	return time.Duration(napDuration)
}

// GetRetryType returns the retry type for the given error
func (e *ExponentialBackoffRetryPolicy) GetRetryType(err error) gocql.RetryType {
	// Retry timeouts and/or contention errors on the same host
	if errors.Is(err, gocql.ErrTimeoutNoResponse) ||
		errors.Is(err, gocql.ErrNoStreams) ||
		errors.Is(err, gocql.ErrTooManyTimeouts) {
		return gocql.Retry
	}

	// Retry next host on unavailable errors
	if errors.Is(err, gocql.ErrUnavailable) ||
		errors.Is(err, gocql.ErrConnectionClosed) ||
		errors.Is(err, gocql.ErrSessionClosed) {
		return gocql.RetryNextHost
	}

	// Otherwise don't retry
	return gocql.Rethrow
}

func delayForAttempt(attempt int) time.Duration {
	if attempt < 50 {
		return time.Millisecond * 5
	}

	return time.Second
}
