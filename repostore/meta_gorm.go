package repostore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	carstore "github.com/bluesky-social/indigo/carstore"
	"github.com/bluesky-social/indigo/models"
	"github.com/ipfs/go-cid"
	"gorm.io/gorm"
)

type CarStoreGormMeta struct {
	meta *gorm.DB
}

func (cs *CarStoreGormMeta) Init() error {
	if err := cs.meta.AutoMigrate(&CarShard{}, &blockRef{}); err != nil {
		return err
	}
	if err := cs.meta.AutoMigrate(&staleRef{}); err != nil {
		return err
	}
	return nil
}

func (cs *CarStoreGormMeta) GetLastShard(ctx context.Context, user models.Uid) (*CarShard, error) {
	var lastShard CarShard
	if err := cs.meta.WithContext(ctx).Model(CarShard{}).Limit(1).Order("seq desc").Find(&lastShard, "usr = ?", user).Error; err != nil {
		return nil, err
	}
	return &lastShard, nil
}

// return all of a users's shards, ascending by Seq
func (cs *CarStoreGormMeta) GetUserShards(ctx context.Context, usr models.Uid) ([]CarShard, error) {
	var shards []CarShard
	if err := cs.meta.Order("seq asc").Find(&shards, "usr = ?", usr).Error; err != nil {
		return nil, err
	}
	return shards, nil
}

// return all of a users's shards, descending by Seq
func (cs *CarStoreGormMeta) GetUserShardsDesc(ctx context.Context, usr models.Uid, minSeq int) ([]CarShard, error) {
	var shards []CarShard
	if err := cs.meta.Order("seq desc").Find(&shards, "usr = ? AND seq >= ?", usr, minSeq).Error; err != nil {
		return nil, err
	}
	return shards, nil
}

func (cs *CarStoreGormMeta) GetUserStaleRefs(ctx context.Context, user models.Uid) ([]staleRef, error) {
	var staleRefs []staleRef
	if err := cs.meta.WithContext(ctx).Find(&staleRefs, "usr = ?", user).Error; err != nil {
		return nil, err
	}
	return staleRefs, nil
}

func (cs *CarStoreGormMeta) SeqForRev(ctx context.Context, user models.Uid, sinceRev string) (int, error) {
	var untilShard CarShard
	if err := cs.meta.Where("rev >= ? AND usr = ?", sinceRev, user).Order("rev").First(&untilShard).Error; err != nil {
		return 0, fmt.Errorf("finding early shard: %w", err)
	}
	return untilShard.Seq, nil
}

func (cs *CarStoreGormMeta) GetCompactionTargets(ctx context.Context, minShardCount int) ([]carstore.CompactionTarget, error) {
	var targets []carstore.CompactionTarget
	if err := cs.meta.Raw(`select usr, count(*) as num_shards from car_shards group by usr having count(*) > ? order by num_shards desc`, minShardCount).Scan(&targets).Error; err != nil {
		return nil, err
	}

	return targets, nil
}

func (cs *CarStoreGormMeta) PutShardAndRefs(ctx context.Context, shard *CarShard, brefs []map[string]any, rmcids map[cid.Cid]bool) error {
	// TODO: there should be a way to create the shard and block_refs that
	// reference it in the same query, would save a lot of time
	tx := cs.meta.WithContext(ctx).Begin()

	if err := tx.WithContext(ctx).Create(shard).Error; err != nil {
		return fmt.Errorf("failed to create shard in DB tx: %w", err)
	}

	if len(rmcids) > 0 {
		cids := make([]cid.Cid, 0, len(rmcids))
		for c := range rmcids {
			cids = append(cids, c)
		}

		if err := tx.Create(&staleRef{
			Cids: packCids(cids),
			Usr:  shard.Usr,
		}).Error; err != nil {
			return err
		}
	}

	err := tx.WithContext(ctx).Commit().Error
	if err != nil {
		return fmt.Errorf("failed to commit shard DB transaction: %w", err)
	}
	return nil
}

func (cs *CarStoreGormMeta) DeleteShardsAndRefs(ctx context.Context, ids []uint) error {

	if err := cs.meta.Delete(&CarShard{}, "id in (?)", ids).Error; err != nil {
		return err
	}

	return nil
}

// valuesStatementForShards builds a postgres compatible statement string from int literals
func valuesStatementForShards(shards []uint) string {
	sb := new(strings.Builder)
	for i, v := range shards {
		sb.WriteByte('(')
		sb.WriteString(strconv.Itoa(int(v)))
		sb.WriteByte(')')
		if i != len(shards)-1 {
			sb.WriteByte(',')
		}
	}
	return sb.String()
}

func (cs *CarStoreGormMeta) SetStaleRef(ctx context.Context, uid models.Uid, staleToKeep []cid.Cid) error {
	txn := cs.meta.Begin()

	if err := txn.Delete(&staleRef{}, "usr = ?", uid).Error; err != nil {
		return err
	}

	// now create a new staleRef with all the refs we couldn't clear out
	if len(staleToKeep) > 0 {
		if err := txn.Create(&staleRef{
			Usr:  uid,
			Cids: packCids(staleToKeep),
		}).Error; err != nil {
			return err
		}
	}

	if err := txn.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit staleRef updates: %w", err)
	}
	return nil
}

type CarShard struct {
	ID        uint `gorm:"primarykey"`
	CreatedAt time.Time

	Root      models.DbCID `gorm:"index"`
	DataStart int64
	Seq       int `gorm:"index:idx_car_shards_seq;index:idx_car_shards_usr_seq,priority:2,sort:desc"`
	Path      string
	Usr       models.Uid `gorm:"index:idx_car_shards_usr;index:idx_car_shards_usr_seq,priority:1"`
	Rev       string
}

type blockRef struct {
	ID     uint         `gorm:"primarykey"`
	Cid    models.DbCID `gorm:"index"`
	Shard  uint         `gorm:"index"`
	Offset int64
	//User   uint `gorm:"index"`
}

type staleRef struct {
	ID   uint `gorm:"primarykey"`
	Cid  *models.DbCID
	Cids []byte
	Usr  models.Uid `gorm:"index"`
}

func (sr *staleRef) getCids() ([]cid.Cid, error) {
	if sr.Cid != nil {
		return []cid.Cid{sr.Cid.CID}, nil
	}

	return unpackCids(sr.Cids)
}

func unpackCids(b []byte) ([]cid.Cid, error) {
	br := bytes.NewReader(b)
	var out []cid.Cid
	for {
		_, c, err := cid.CidFromReader(br)
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		out = append(out, c)
	}

	return out, nil
}

func packCids(cids []cid.Cid) []byte {
	buf := new(bytes.Buffer)
	for _, c := range cids {
		buf.Write(c.Bytes())
	}

	return buf.Bytes()
}
