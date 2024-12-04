package carstore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/bluesky-social/indigo/models"
	blockformat "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	car "github.com/ipld/go-car"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type NonArchivalCarstore struct {
	db *gorm.DB

	lk              sync.Mutex
	lastCommitCache map[models.Uid]*commitRefInfo
}

func NewNonArchivalCarstore(db *gorm.DB) (*NonArchivalCarstore, error) {
	if err := db.AutoMigrate(&commitRefInfo{}); err != nil {
		return nil, err
	}

	return &NonArchivalCarstore{
		db:              db,
		lastCommitCache: make(map[models.Uid]*commitRefInfo),
	}, nil
}

type commitRefInfo struct {
	ID   uint       `gorm:"primarykey"`
	Uid  models.Uid `gorm:"uniqueIndex"`
	Rev  string
	Root models.DbCID
}

func (cs *NonArchivalCarstore) checkLastShardCache(user models.Uid) *commitRefInfo {
	cs.lk.Lock()
	defer cs.lk.Unlock()

	ls, ok := cs.lastCommitCache[user]
	if ok {
		return ls
	}

	return nil
}

func (cs *NonArchivalCarstore) removeLastShardCache(user models.Uid) {
	cs.lk.Lock()
	defer cs.lk.Unlock()

	delete(cs.lastCommitCache, user)
}

func (cs *NonArchivalCarstore) putLastShardCache(ls *commitRefInfo) {
	cs.lk.Lock()
	defer cs.lk.Unlock()

	cs.lastCommitCache[ls.Uid] = ls
}

func (cs *NonArchivalCarstore) loadCommitRefInfo(ctx context.Context, user models.Uid) (*commitRefInfo, error) {
	var out commitRefInfo
	if err := cs.db.Find(&out, "uid = ?", user).Error; err != nil {
		return nil, err
	}

	return &out, nil
}

func (cs *NonArchivalCarstore) getCommitRefInfo(ctx context.Context, user models.Uid) (*commitRefInfo, error) {
	ctx, span := otel.Tracer("carstore").Start(ctx, "getCommitRefInfo")
	defer span.End()

	maybeLs := cs.checkLastShardCache(user)
	if maybeLs != nil {
		return maybeLs, nil
	}

	lastShard, err := cs.loadCommitRefInfo(ctx, user)
	if err != nil {
		return nil, err
	}

	cs.putLastShardCache(lastShard)
	return lastShard, nil
}

func (cs *NonArchivalCarstore) updateLastCommit(ctx context.Context, uid models.Uid, rev string, cid cid.Cid) error {
	cri := &commitRefInfo{
		Uid:  uid,
		Rev:  rev,
		Root: models.DbCID{cid},
	}

	if err := cs.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "uid"}},
		UpdateAll: true,
	}).Create(cri).Error; err != nil {
		return fmt.Errorf("update or set last commit info: %w", err)
	}

	cs.putLastShardCache(cri)

	return nil
}

func (cs *NonArchivalCarstore) NewDeltaSession(ctx context.Context, user models.Uid, since *string) (*DeltaSession, error) {
	ctx, span := otel.Tracer("carstore").Start(ctx, "NewSession")
	defer span.End()

	// TODO: ensure that we don't write updates on top of the wrong head
	// this needs to be a compare and swap type operation
	lastShard, err := cs.getCommitRefInfo(ctx, user)
	if err != nil {
		return nil, err
	}

	if since != nil && *since != lastShard.Rev {
		log.Warn("revision mismatch: %s != %s: %s", *since, lastShard.Rev, ErrRepoBaseMismatch)
	}

	return &DeltaSession{
		fresh: blockstore.NewBlockstore(datastore.NewMapDatastore()),
		blks:  make(map[cid.Cid]blockformat.Block),
		base: &userView{
			user:     user,
			cs:       cs,
			prefetch: true,
			cache:    make(map[cid.Cid]blockformat.Block),
		},
		user:    user,
		baseCid: lastShard.Root.CID,
		cs:      cs,
		seq:     0,
		lastRev: lastShard.Rev,
	}, nil
}

func (cs *NonArchivalCarstore) ReadOnlySession(user models.Uid) (*DeltaSession, error) {
	return &DeltaSession{
		base: &userView{
			user:     user,
			cs:       cs,
			prefetch: false,
			cache:    make(map[cid.Cid]blockformat.Block),
		},
		readonly: true,
		user:     user,
		cs:       cs,
	}, nil
}

// TODO: incremental is only ever called true, remove the param
func (cs *NonArchivalCarstore) ReadUserCar(ctx context.Context, user models.Uid, sinceRev string, incremental bool, w io.Writer) error {
	return fmt.Errorf("not supported in non-archival mode")
}

func (cs *NonArchivalCarstore) ImportSlice(ctx context.Context, uid models.Uid, since *string, carslice []byte) (cid.Cid, *DeltaSession, error) {
	ctx, span := otel.Tracer("carstore").Start(ctx, "ImportSlice")
	defer span.End()

	carr, err := car.NewCarReader(bytes.NewReader(carslice))
	if err != nil {
		return cid.Undef, nil, err
	}

	if len(carr.Header.Roots) != 1 {
		return cid.Undef, nil, fmt.Errorf("invalid car file, header must have a single root (has %d)", len(carr.Header.Roots))
	}

	ds, err := cs.NewDeltaSession(ctx, uid, since)
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

func (cs *NonArchivalCarstore) GetUserRepoHead(ctx context.Context, user models.Uid) (cid.Cid, error) {
	lastShard, err := cs.getCommitRefInfo(ctx, user)
	if err != nil {
		return cid.Undef, err
	}
	if lastShard.ID == 0 {
		return cid.Undef, nil
	}

	return lastShard.Root.CID, nil
}

func (cs *NonArchivalCarstore) GetUserRepoRev(ctx context.Context, user models.Uid) (string, error) {
	lastShard, err := cs.getCommitRefInfo(ctx, user)
	if err != nil {
		return "", err
	}
	if lastShard.ID == 0 {
		return "", nil
	}

	return lastShard.Rev, nil
}

func (cs *NonArchivalCarstore) Stat(ctx context.Context, usr models.Uid) ([]UserStat, error) {
	return nil, nil
}

func (cs *NonArchivalCarstore) WipeUserData(ctx context.Context, user models.Uid) error {
	if err := cs.db.Raw("DELETE from commit_ref_infos WHERE uid = ?", user).Error; err != nil {
		return err
	}

	cs.removeLastShardCache(user)
	return nil
}

func (cs *NonArchivalCarstore) GetCompactionTargets(ctx context.Context, shardCount int) ([]CompactionTarget, error) {
	return nil, fmt.Errorf("compaction not supported on non-archival")
}

func (cs *NonArchivalCarstore) CompactUserShards(ctx context.Context, user models.Uid, skipBigShards bool) (*CompactionStats, error) {
	return nil, fmt.Errorf("compaction not supported in non-archival")
}
