package carstore

import (
	"bytes"
	"context"
	"fmt"
	ipld "github.com/ipfs/go-ipld-format"
	"io"
	"log/slog"
	"sync"

	"github.com/bluesky-social/indigo/models"
	blockformat "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	car "github.com/ipld/go-car"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type NonArchivalCarstore struct {
	db *gorm.DB

	lk              sync.Mutex
	lastCommitCache map[models.Uid]*commitRefInfo

	log *slog.Logger
}

func NewNonArchivalCarstore(db *gorm.DB) (*NonArchivalCarstore, error) {
	if err := db.AutoMigrate(&commitRefInfo{}); err != nil {
		return nil, err
	}

	return &NonArchivalCarstore{
		db:              db,
		lastCommitCache: make(map[models.Uid]*commitRefInfo),
		log:             slog.Default().With("system", "carstorena"),
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
	wat := cs.db.Find(&out, "uid = ?", user)
	if wat.Error != nil {
		return nil, wat.Error
	}
	if wat.RowsAffected == 0 {
		return nil, nil
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
	if lastShard == nil {
		return nil, nil
	}

	cs.putLastShardCache(lastShard)
	return lastShard, nil
}

func (cs *NonArchivalCarstore) updateLastCommit(ctx context.Context, uid models.Uid, rev string, cid cid.Cid) error {
	cri := &commitRefInfo{
		Uid:  uid,
		Rev:  rev,
		Root: models.DbCID{CID: cid},
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

var commitRefZero = commitRefInfo{}

func (cs *NonArchivalCarstore) NewDeltaSession(ctx context.Context, user models.Uid, since *string) (*DeltaSession, error) {
	ctx, span := otel.Tracer("carstore").Start(ctx, "NewSession")
	defer span.End()

	// TODO: ensure that we don't write updates on top of the wrong head
	// this needs to be a compare and swap type operation
	lastShard, err := cs.getCommitRefInfo(ctx, user)
	if err != nil {
		return nil, err
	}

	if lastShard == nil {
		// ok, no previous user state to refer to
		lastShard = &commitRefZero
	} else if since != nil && *since != lastShard.Rev {
		cs.log.Warn("revision mismatch", "commitSince", since, "lastRev", lastShard.Rev, "err", ErrRepoBaseMismatch)
	}

	return &DeltaSession{
		blks: make(map[cid.Cid]blockformat.Block),
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
	if lastShard == nil || lastShard.ID == 0 {
		return cid.Undef, nil
	}

	return lastShard.Root.CID, nil
}

func (cs *NonArchivalCarstore) GetUserRepoRev(ctx context.Context, user models.Uid) (string, error) {
	lastShard, err := cs.getCommitRefInfo(ctx, user)
	if err != nil {
		return "", err
	}
	if lastShard == nil || lastShard.ID == 0 {
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

func (cs *NonArchivalCarstore) HasUidCid(ctx context.Context, user models.Uid, k cid.Cid) (bool, error) {
	return false, nil
}

func (cs *NonArchivalCarstore) LookupBlockRef(ctx context.Context, k cid.Cid) (path string, offset int64, user models.Uid, err error) {
	return "", 0, 0, ipld.ErrNotFound{Cid: k}
}

func (cs *NonArchivalCarstore) writeNewShard(ctx context.Context, root cid.Cid, rev string, user models.Uid, seq int, blks map[cid.Cid]blockformat.Block, rmcids map[cid.Cid]bool) ([]byte, error) {
	slice, err := blocksToCar(ctx, root, rev, blks)
	if err != nil {
		return nil, err
	}
	return slice, cs.updateLastCommit(ctx, user, rev, root)
}
