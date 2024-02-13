package carstore

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/bluesky-social/indigo/models"
	"github.com/ipfs/go-cid"
	ipld "github.com/ipfs/go-ipld-format"
	car "github.com/ipld/go-car"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

func NewPostgresBackend(db *gorm.DB) (Backend, error) {
	if err := db.AutoMigrate(&CarShard{}, &blockRef{}); err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&staleRef{}); err != nil {
		return nil, err
	}

	return &PgBackend{
		db: db,
	}, nil
}

type PgBackend struct {
	db *gorm.DB
}

func (pgb *PgBackend) UserHasBlock(ctx context.Context, usr models.Uid, blk cid.Cid) (bool, error) {
	var count int64
	if err := pgb.db.
		Model(blockRef{}).
		Select("path, block_refs.offset").
		Joins("left join car_shards on block_refs.shard = car_shards.id").
		Where("usr = ? AND cid = ?", usr, models.DbCID{CID: blk}).
		Count(&count).Error; err != nil {
		return false, err
	}

	return count > 0, nil
}

func (pgb *PgBackend) FindBlock(ctx context.Context, usr models.Uid, k cid.Cid) (string, int64, bool, error) {
	// TODO: for now, im using a join to ensure we only query blocks from the
	// correct user. maybe it makes sense to put the user in the blockRef
	// directly? tradeoff of time vs space
	var info struct {
		Path   string
		Offset int64
		Usr    models.Uid
	}
	if err := pgb.db.Raw(`SELECT
  (select path from car_shards where id = block_refs.shard) as path,
  block_refs.offset,
  (select usr from car_shards where id = block_refs.shard) as usr
FROM block_refs
WHERE
  block_refs.cid = ?
LIMIT 1;`, models.DbCID{CID: k}).Scan(&info).Error; err != nil {
		return "", 0, false, err
	}
	if info.Path == "" {
		return "", 0, false, ipld.ErrNotFound{Cid: k}
	}

	return info.Path, info.Offset, usr == info.Usr, nil
}

func (pgb *PgBackend) GetLastShard(ctx context.Context, user models.Uid) (*CarShard, error) {
	var lastShard CarShard
	// this is often slow (which is why we're caching it) but could be sped up with an extra index:
	// CREATE INDEX idx_car_shards_usr_id ON car_shards (usr, seq DESC);
	if err := pgb.db.WithContext(ctx).Model(CarShard{}).Limit(1).Order("seq desc").Find(&lastShard, "usr = ?", user).Error; err != nil {
		//if err := cs.meta.Model(CarShard{}).Where("user = ?", user).Last(&lastShard).Error; err != nil {
		//if err != gorm.ErrRecordNotFound {
		return nil, err
		//}
	}

	return &lastShard, nil
}

func (pgb *PgBackend) ReadUserCar(ctx context.Context, user models.Uid, sinceRev string, incremental bool, w io.Writer) error {
	var earlySeq int
	if sinceRev != "" {
		var untilShard CarShard
		if err := pgb.db.Where("rev >= ? AND usr = ?", sinceRev, user).Order("rev").First(&untilShard).Error; err != nil {
			return fmt.Errorf("finding early shard: %w", err)
		}
		earlySeq = untilShard.Seq
	}

	var shards []CarShard
	if err := pgb.db.Order("seq desc").Where("usr = ? AND seq >= ?", user, earlySeq).Find(&shards).Error; err != nil {
		return err
	}

	if !incremental && earlySeq > 0 {
		// have to do it the ugly way
		return fmt.Errorf("nyi")
	}

	if len(shards) == 0 {
		return fmt.Errorf("no data found for user %d", user)
	}

	// fast path!
	if err := car.WriteHeader(&car.CarHeader{
		Roots:   []cid.Cid{shards[0].Root.CID},
		Version: 1,
	}, w); err != nil {
		return err
	}

	for _, sh := range shards {
		if err := writeShardBlocks(ctx, &sh, w); err != nil {
			return err
		}
	}

	return nil
}

func (pgb *PgBackend) PutShard(ctx context.Context, shard *CarShard, brefs []map[string]any, rmcids map[cid.Cid]bool) error {
	// TODO: there should be a way to create the shard and block_refs that
	// reference it in the same query, would save a lot of time
	tx := pgb.db.WithContext(ctx).Begin()

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

func createBlockRefs(ctx context.Context, tx *gorm.DB, brefs []map[string]any) error {
	ctx, span := otel.Tracer("carstore").Start(ctx, "createBlockRefs")
	defer span.End()

	if err := createInBatches(ctx, tx, brefs, 2000); err != nil {
		return err
	}

	return nil
}

func generateInsertQuery(data []map[string]any) (string, []any) {
	placeholders := strings.Repeat("(?, ?, ?),", len(data))
	placeholders = placeholders[:len(placeholders)-1] // trim trailing comma

	query := "INSERT INTO block_refs (\"cid\", \"offset\", \"shard\") VALUES " + placeholders

	values := make([]any, 0, 3*len(data))
	for _, entry := range data {
		values = append(values, entry["cid"], entry["offset"], entry["shard"])
	}

	return query, values
}

// Function to create in batches
func createInBatches(ctx context.Context, tx *gorm.DB, data []map[string]any, batchSize int) error {
	for i := 0; i < len(data); i += batchSize {
		batch := data[i:]
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

func (pgb *PgBackend) GetAllShards(ctx context.Context, usr models.Uid) ([]CarShard, error) {
	var shards []CarShard
	if err := pgb.db.Order("seq asc").Find(&shards, "usr = ?", usr).Error; err != nil {
		return nil, err
	}
	return shards, nil
}

func (pgb *PgBackend) WipeUserData(ctx context.Context, user models.Uid) error {
	var shards []*CarShard
	if err := pgb.db.Find(&shards, "usr = ?", user).Error; err != nil {
		return err
	}

	if err := pgb.deleteShards(ctx, shards); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
	}

	return nil
}

func (pgb *PgBackend) deleteShards(ctx context.Context, shs []*CarShard) error {
	ctx, span := otel.Tracer("carstore").Start(ctx, "deleteShards")
	defer span.End()

	deleteSlice := func(ctx context.Context, subs []*CarShard) error {
		var ids []uint
		for _, sh := range subs {
			ids = append(ids, sh.ID)
		}

		txn := pgb.db.Begin()

		if err := txn.Delete(&CarShard{}, "id in (?)", ids).Error; err != nil {
			return err
		}

		if err := txn.Delete(&blockRef{}, "shard in (?)", ids).Error; err != nil {
			return err
		}

		if err := txn.Commit().Error; err != nil {
			return err
		}

		var outErr error
		for _, sh := range subs {
			if err := deleteShardFile(ctx, sh); err != nil {
				if !os.IsNotExist(err) {
					return err
				}
				outErr = err
			}
		}

		return outErr
	}

	chunkSize := 10000
	for i := 0; i < len(shs); i += chunkSize {
		sl := shs[i:]
		if len(sl) > chunkSize {
			sl = sl[:chunkSize]
		}

		if err := deleteSlice(ctx, sl); err != nil {
			return err
		}
	}

	return nil
}

func (pgb *PgBackend) GetCompactionTargets(ctx context.Context, shardCount int) ([]CompactionTarget, error) {
	var targets []CompactionTarget
	if err := pgb.db.Raw(`select usr, count(*) as num_shards from car_shards group by usr having count(*) > ? order by num_shards desc`, shardCount).Scan(&targets).Error; err != nil {
		return nil, err
	}

	return targets, nil
}

func (pgb *PgBackend) GetBlockRefsForShards(ctx context.Context, shardIds []uint) ([]blockRef, error) {
	chunkSize := 10000
	out := make([]blockRef, 0, len(shardIds))
	for i := 0; i < len(shardIds); i += chunkSize {
		sl := shardIds[i:]
		if len(sl) > chunkSize {
			sl = sl[:chunkSize]
		}

		var brefs []blockRef
		if err := pgb.db.Raw(`select * from block_refs where shard in (?)`, sl).Scan(&brefs).Error; err != nil {
			return nil, err
		}

		out = append(out, brefs...)
	}

	return out, nil
}

func (pgb *PgBackend) DeleteStaleRefs(ctx context.Context, uid models.Uid, brefs []blockRef, staleRefs []staleRef, removedShards map[uint]bool) error {
	ctx, span := otel.Tracer("pgbackend").Start(ctx, "deleteStaleRefs")
	defer span.End()

	brByCid := make(map[cid.Cid][]blockRef)
	for _, br := range brefs {
		brByCid[br.Cid.CID] = append(brByCid[br.Cid.CID], br)
	}

	var staleToKeep []cid.Cid
	for _, sr := range staleRefs {
		cids, err := sr.getCids()
		if err != nil {
			return fmt.Errorf("getCids on staleRef failed (%d): %w", sr.ID, err)
		}

		for _, c := range cids {
			brs := brByCid[c]
			del := true
			for _, br := range brs {
				if !removedShards[br.Shard] {
					del = false
					break
				}
			}

			if !del {
				staleToKeep = append(staleToKeep, c)
			}
		}
	}

	txn := pgb.db.Begin()

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

func (pgb *PgBackend) GetStaleRefs(ctx context.Context, user models.Uid) ([]staleRef, error) {
	var staleRefs []staleRef
	if err := pgb.db.WithContext(ctx).Find(&staleRefs, "usr = ?", user).Error; err != nil {
		return nil, err
	}
	return staleRefs, nil
}
