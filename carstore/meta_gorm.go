package carstore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/models"
	"github.com/ipfs/go-cid"
	"go.opentelemetry.io/otel"
	"gorm.io/driver/postgres"
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

// Return true if any known record matches (Uid, Cid)
func (cs *CarStoreGormMeta) HasUidCid(ctx context.Context, user models.Uid, k cid.Cid) (bool, error) {
	var count int64
	if err := cs.meta.
		Model(blockRef{}).
		Select("path, block_refs.offset").
		Joins("left join car_shards on block_refs.shard = car_shards.id").
		Where("usr = ? AND cid = ?", user, models.DbCID{CID: k}).
		Count(&count).Error; err != nil {
		return false, err
	}

	return count > 0, nil
}

// For some Cid, lookup the block ref.
// Return the path of the file written, the offset within the file, and the user associated with the Cid.
func (cs *CarStoreGormMeta) LookupBlockRef(ctx context.Context, k cid.Cid) (path string, offset int64, user models.Uid, err error) {
	// TODO: for now, im using a join to ensure we only query blocks from the
	// correct user. maybe it makes sense to put the user in the blockRef
	// directly? tradeoff of time vs space
	var info struct {
		Path   string
		Offset int64
		Usr    models.Uid
	}
	if err := cs.meta.Raw(`SELECT
  (select path from car_shards where id = block_refs.shard) as path,
  block_refs.offset,
  (select usr from car_shards where id = block_refs.shard) as usr
FROM block_refs
WHERE
  block_refs.cid = ?
LIMIT 1;`, models.DbCID{CID: k}).Scan(&info).Error; err != nil {
		var defaultUser models.Uid
		return "", -1, defaultUser, err
	}
	return info.Path, info.Offset, info.Usr, nil
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

func (cs *CarStoreGormMeta) GetCompactionTargets(ctx context.Context, minShardCount int) ([]CompactionTarget, error) {
	var targets []CompactionTarget
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

	for _, ref := range brefs {
		ref["shard"] = shard.ID
	}

	if err := createBlockRefs(ctx, tx, brefs); err != nil {
		return fmt.Errorf("failed to create block refs: %w", err)
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
	txn := cs.meta.Begin()

	if err := txn.Delete(&CarShard{}, "id in (?)", ids).Error; err != nil {
		txn.Rollback()
		return err
	}

	if err := txn.Delete(&blockRef{}, "shard in (?)", ids).Error; err != nil {
		txn.Rollback()
		return err
	}

	return txn.Commit().Error
}

func (cs *CarStoreGormMeta) GetBlockRefsForShards(ctx context.Context, shardIds []uint) ([]blockRef, error) {
	chunkSize := 2000
	out := make([]blockRef, 0, len(shardIds))
	for i := 0; i < len(shardIds); i += chunkSize {
		sl := shardIds[i:]
		if len(sl) > chunkSize {
			sl = sl[:chunkSize]
		}

		if err := blockRefsForShards(ctx, cs.meta, sl, &out); err != nil {
			return nil, fmt.Errorf("getting block refs: %w", err)
		}
	}
	return out, nil
}

// blockRefsForShards is an inner loop helper for GetBlockRefsForShards
func blockRefsForShards(ctx context.Context, db *gorm.DB, shards []uint, obuf *[]blockRef) error {
	// Check the database driver
	switch db.Dialector.(type) {
	case *postgres.Dialector:
		sval := valuesStatementForShards(shards)
		q := fmt.Sprintf(`SELECT block_refs.* FROM block_refs INNER JOIN (VALUES %s) AS vals(v) ON block_refs.shard = v`, sval)
		return db.Raw(q).Scan(obuf).Error
	default:
		return db.Raw(`SELECT * FROM block_refs WHERE shard IN (?)`, shards).Scan(obuf).Error
	}
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

func createBlockRefs(ctx context.Context, tx *gorm.DB, brefs []map[string]any) error {
	ctx, span := otel.Tracer("carstore").Start(ctx, "createBlockRefs")
	defer span.End()

	if err := createInBatches(ctx, tx, brefs, 2000); err != nil {
		return err
	}

	return nil
}

// Function to create in batches
func createInBatches(ctx context.Context, tx *gorm.DB, brefs []map[string]any, batchSize int) error {
	for i := 0; i < len(brefs); i += batchSize {
		batch := brefs[i:]
		if len(batch) > batchSize {
			batch = batch[:batchSize]
		}

		query, values := generateInsertQuery(batch)

		if err := tx.WithContext(ctx).Exec(query, values...).Error; err != nil {
			return err
		}
	}
	return nil
}

func generateInsertQuery(brefs []map[string]any) (string, []any) {
	placeholders := strings.Repeat("(?, ?, ?),", len(brefs))
	placeholders = placeholders[:len(placeholders)-1] // trim trailing comma

	query := "INSERT INTO block_refs (\"cid\", \"offset\", \"shard\") VALUES " + placeholders

	values := make([]any, 0, 3*len(brefs))
	for _, entry := range brefs {
		values = append(values, entry["cid"], entry["offset"], entry["shard"])
	}

	return query, values
}
