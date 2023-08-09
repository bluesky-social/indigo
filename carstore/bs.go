package carstore

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/models"

	blockformat "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	cbor "github.com/ipfs/go-ipld-cbor"
	ipld "github.com/ipfs/go-ipld-format"
	"github.com/ipfs/go-libipfs/blocks"
	car "github.com/ipld/go-car"
	carutil "github.com/ipld/go-car/util"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

const MaxSliceLength = 2 << 20

type CarStore struct {
	meta    *gorm.DB
	rootDir string

	lscLk          sync.Mutex
	lastShardCache map[models.Uid]*CarShard
}

func NewCarStore(meta *gorm.DB, root string) (*CarStore, error) {
	if _, err := os.Stat(root); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}

		if err := os.Mkdir(root, 0775); err != nil {
			return nil, err
		}
	}
	if err := meta.AutoMigrate(&CarShard{}, &blockRef{}); err != nil {
		return nil, err
	}
	return &CarStore{
		meta:           meta,
		rootDir:        root,
		lastShardCache: make(map[models.Uid]*CarShard),
	}, nil
}

type UserInfo struct {
	gorm.Model
	Head string
}

type CarShard struct {
	ID        uint `gorm:"primarykey"`
	CreatedAt time.Time

	Root      models.DbCID `gorm:"index"`
	DataStart int64
	Seq       int `gorm:"index:idx_car_shards_seq;index:idx_car_shards_usr_seq,priority:2,sort:desc"`
	Path      string
	Usr       models.Uid `gorm:"index:idx_car_shards_usr;index:idx_car_shards_usr_seq,priority:1"`
	Rebase    bool
}

type blockRef struct {
	ID     uint         `gorm:"primarykey"`
	Cid    models.DbCID `gorm:"index"`
	Shard  uint
	Offset int64
	//User   uint `gorm:"index"`
}

type userView struct {
	cs   *CarStore
	user models.Uid

	cache    map[cid.Cid]blockformat.Block
	prefetch bool
}

var _ blockstore.Blockstore = (*userView)(nil)

func (uv *userView) HashOnRead(hor bool) {
	//noop
}

func (uv *userView) Has(ctx context.Context, k cid.Cid) (bool, error) {
	var count int64
	if err := uv.cs.meta.
		Model(blockRef{}).
		Select("path, block_refs.offset").
		Joins("left join car_shards on block_refs.shard = car_shards.id").
		Where("usr = ? AND cid = ?", uv.user, models.DbCID{k}).
		Count(&count).Error; err != nil {
		return false, err
	}

	return count > 0, nil
}

func (uv *userView) Get(ctx context.Context, k cid.Cid) (blockformat.Block, error) {
	if !k.Defined() {
		return nil, fmt.Errorf("attempted to 'get' undefined cid")
	}
	if uv.cache != nil {
		blk, ok := uv.cache[k]
		if ok {
			return blk, nil
		}
	}

	// TODO: for now, im using a join to ensure we only query blocks from the
	// correct user. maybe it makes sense to put the user in the blockRef
	// directly? tradeoff of time vs space
	var info struct {
		Path   string
		Offset int64
	}
	if err := uv.cs.meta.
		Model(blockRef{}).
		Select("path, block_refs.offset").
		Joins("left join car_shards on block_refs.shard = car_shards.id").
		Where("usr = ? AND cid = ?", uv.user, models.DbCID{k}).
		Find(&info).Error; err != nil {
		return nil, err
	}
	if info.Path == "" {
		return nil, ipld.ErrNotFound{k}
	}

	if uv.prefetch {
		return uv.prefetchRead(ctx, k, info.Path, info.Offset)
	} else {
		return uv.singleRead(ctx, k, info.Path, info.Offset)
	}
}

func (uv *userView) prefetchRead(ctx context.Context, k cid.Cid, path string, offset int64) (blockformat.Block, error) {
	fi, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fi.Close()

	cr, err := car.NewCarReader(fi)
	if err != nil {
		return nil, err
	}

	for {
		blk, err := cr.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		uv.cache[blk.Cid()] = blk
	}

	outblk, ok := uv.cache[k]
	if !ok {
		return nil, fmt.Errorf("requested block was not found in car slice")
	}

	return outblk, nil
}

func (uv *userView) singleRead(ctx context.Context, k cid.Cid, path string, offset int64) (blockformat.Block, error) {
	fi, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fi.Close()

	seeked, err := fi.Seek(offset, io.SeekStart)
	if err != nil {
		return nil, err
	}

	if seeked != offset {
		return nil, fmt.Errorf("failed to seek to offset (%d != %d)", seeked, offset)
	}

	bufr := bufio.NewReader(fi)
	rcid, data, err := carutil.ReadNode(bufr)
	if err != nil {
		return nil, err
	}

	if rcid != k {
		return nil, fmt.Errorf("mismatch in cid on disk: %s != %s", rcid, k)
	}

	return blocks.NewBlockWithCid(data, rcid)
}

func (uv *userView) AllKeysChan(ctx context.Context) (<-chan cid.Cid, error) {
	return nil, fmt.Errorf("not implemented")
}

func (uv *userView) Put(ctx context.Context, blk blockformat.Block) error {
	return fmt.Errorf("puts not supported to car view blockstores")
}

func (uv *userView) PutMany(ctx context.Context, blks []blockformat.Block) error {
	return fmt.Errorf("puts not supported to car view blockstores")
}

func (uv *userView) DeleteBlock(ctx context.Context, k cid.Cid) error {
	return fmt.Errorf("deletes not supported to car view blockstore")
}

func (uv *userView) GetSize(ctx context.Context, k cid.Cid) (int, error) {
	// TODO: maybe block size should be in the database record...
	blk, err := uv.Get(ctx, k)
	if err != nil {
		return 0, err
	}

	return len(blk.RawData()), nil
}

type DeltaSession struct {
	fresh    blockstore.Blockstore
	blks     map[cid.Cid]blockformat.Block
	base     blockstore.Blockstore
	user     models.Uid
	seq      int
	readonly bool
	cs       *CarStore
}

func (cs *CarStore) checkLastShardCache(user models.Uid) *CarShard {
	cs.lscLk.Lock()
	defer cs.lscLk.Unlock()

	ls, ok := cs.lastShardCache[user]
	if ok {
		return ls
	}

	return nil
}

func (cs *CarStore) putLastShardCache(user models.Uid, ls *CarShard) {
	cs.lscLk.Lock()
	defer cs.lscLk.Unlock()

	cs.lastShardCache[user] = ls
}

func (cs *CarStore) getLastShard(ctx context.Context, user models.Uid) (*CarShard, error) {
	ctx, span := otel.Tracer("carstore").Start(ctx, "getLastShard")
	defer span.End()

	maybeLs := cs.checkLastShardCache(user)
	if maybeLs != nil {
		return maybeLs, nil
	}

	var lastShard CarShard
	// this is often slow (which is why we're caching it) but could be sped up with an extra index:
	// CREATE INDEX idx_car_shards_usr_id ON car_shards (usr, seq DESC);
	if err := cs.meta.WithContext(ctx).Model(CarShard{}).Limit(1).Order("seq desc").Find(&lastShard, "usr = ?", user).Error; err != nil {
		//if err := cs.meta.Model(CarShard{}).Where("user = ?", user).Last(&lastShard).Error; err != nil {
		//if err != gorm.ErrRecordNotFound {
		return nil, err
		//}
	}

	cs.putLastShardCache(user, &lastShard)
	return &lastShard, nil
}

var ErrRepoBaseMismatch = fmt.Errorf("attempted a delta session on top of the wrong previous head")

var ErrRepoFork = fmt.Errorf("repo fork detected")

func (cs *CarStore) NewDeltaSession(ctx context.Context, user models.Uid, prev *cid.Cid) (*DeltaSession, error) {
	ctx, span := otel.Tracer("carstore").Start(ctx, "NewSession")
	defer span.End()

	// TODO: ensure that we don't write updates on top of the wrong head
	// this needs to be a compare and swap type operation
	lastShard, err := cs.getLastShard(ctx, user)
	if err != nil {
		return nil, err
	}

	if prev != nil {
		if lastShard.Root.CID != *prev {
			fork, err := cs.checkFork(ctx, user, *prev)
			if err != nil {
				return nil, fmt.Errorf("failed to check carstore base mismatch for fork condition: %w", err)
			}

			if fork {
				return nil, fmt.Errorf("fork at %s: %w", prev.String(), ErrRepoFork)
			}

			return nil, fmt.Errorf("mismatch: %s != %s: %w", lastShard.Root.CID, prev.String(), ErrRepoBaseMismatch)
		}
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
		user: user,
		cs:   cs,
		seq:  lastShard.Seq + 1,
	}, nil
}

func (cs *CarStore) ReadOnlySession(user models.Uid) (*DeltaSession, error) {
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

func (cs *CarStore) ReadUserCar(ctx context.Context, user models.Uid, earlyCid, lateCid cid.Cid, incremental bool, w io.Writer) error {
	ctx, span := otel.Tracer("carstore").Start(ctx, "ReadUserCar")
	defer span.End()

	var lateSeq, earlySeq int

	if earlyCid.Defined() {
		var untilShard CarShard
		if err := cs.meta.First(&untilShard, "root = ? AND usr = ?", models.DbCID{earlyCid}, user).Error; err != nil {
			return fmt.Errorf("finding early shard: %w", err)
		}
		earlySeq = untilShard.Seq
	}

	if lateCid.Defined() {
		var fromShard CarShard
		if err := cs.meta.First(&fromShard, "root = ? AND usr = ?", models.DbCID{lateCid}, user).Error; err != nil {
			return fmt.Errorf("finding late shard: %w", err)
		}
		lateSeq = fromShard.Seq
	}

	q := cs.meta.Order("seq desc").Where("usr = ? AND seq > ?", user, earlySeq)
	if lateCid.Defined() {
		q = q.Where("seq <= ?", lateSeq)
	}
	var shards []CarShard
	if err := q.Find(&shards).Error; err != nil {
		return err
	}

	if !incremental && earlyCid.Defined() {
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
		// for rebase shards, only include the modified root, not the whole tree
		if sh.Rebase && incremental {
			if err := cs.writeBlockFromShard(ctx, &sh, w, sh.Root.CID); err != nil {
				return err
			}
		} else {
			if err := cs.writeShardBlocks(ctx, &sh, w); err != nil {
				return err
			}
		}
	}

	return nil
}

func (cs *CarStore) writeShardBlocks(ctx context.Context, sh *CarShard, w io.Writer) error {
	ctx, span := otel.Tracer("carstore").Start(ctx, "writeShardBlocks")
	defer span.End()

	fi, err := os.Open(sh.Path)
	if err != nil {
		return err
	}
	defer fi.Close()

	_, err = fi.Seek(sh.DataStart, io.SeekStart)
	if err != nil {
		return err
	}

	_, err = io.Copy(w, fi)
	if err != nil {
		return err
	}

	return nil
}

func (cs *CarStore) writeBlockFromShard(ctx context.Context, sh *CarShard, w io.Writer, c cid.Cid) error {
	fi, err := os.Open(sh.Path)
	if err != nil {
		return err
	}
	defer fi.Close()

	rr, err := car.NewCarReader(fi)
	if err != nil {
		return err
	}

	for {
		blk, err := rr.Next()
		if err != nil {
			return err
		}

		if blk.Cid() == c {
			_, err := LdWrite(w, c.Bytes(), blk.RawData())
			return err
		}
	}
}

var _ blockstore.Blockstore = (*DeltaSession)(nil)

func (ds *DeltaSession) Put(ctx context.Context, b blockformat.Block) error {
	if ds.readonly {
		return fmt.Errorf("cannot write to readonly deltaSession")
	}
	ds.blks[b.Cid()] = b
	return nil
}

func (ds *DeltaSession) PutMany(ctx context.Context, bs []blockformat.Block) error {
	if ds.readonly {
		return fmt.Errorf("cannot write to readonly deltaSession")
	}

	for _, b := range bs {
		ds.blks[b.Cid()] = b
	}
	return nil
}

func (ds *DeltaSession) AllKeysChan(ctx context.Context) (<-chan cid.Cid, error) {
	return nil, fmt.Errorf("AllKeysChan not implemented")
}

func (ds *DeltaSession) DeleteBlock(ctx context.Context, c cid.Cid) error {
	if ds.readonly {
		return fmt.Errorf("cannot write to readonly deltaSession")
	}

	if _, ok := ds.blks[c]; !ok {
		return ipld.ErrNotFound{c}
	}

	delete(ds.blks, c)
	return nil
}

func (ds *DeltaSession) Get(ctx context.Context, c cid.Cid) (blockformat.Block, error) {
	b, ok := ds.blks[c]
	if ok {
		return b, nil
	}

	return ds.base.Get(ctx, c)
}

func (ds *DeltaSession) Has(ctx context.Context, c cid.Cid) (bool, error) {
	_, ok := ds.blks[c]
	if ok {
		return true, nil
	}

	return ds.base.Has(ctx, c)
}

func (ds *DeltaSession) HashOnRead(hor bool) {
	// noop?
}

func (ds *DeltaSession) GetSize(ctx context.Context, c cid.Cid) (int, error) {
	b, ok := ds.blks[c]
	if ok {
		return len(b.RawData()), nil
	}

	return ds.base.GetSize(ctx, c)
}

func fnameForShard(user models.Uid, seq int) string {
	return fmt.Sprintf("sh-%d-%d", user, seq)
}
func (cs *CarStore) openNewShardFile(ctx context.Context, user models.Uid, seq int) (*os.File, string, error) {
	// TODO: some overwrite protections
	fname := filepath.Join(cs.rootDir, fnameForShard(user, seq))
	fi, err := os.Create(fname)
	if err != nil {
		return nil, "", err
	}

	return fi, fname, nil
}

func (cs *CarStore) writeNewShardFile(ctx context.Context, user models.Uid, seq int, data []byte) (string, error) {
	_, span := otel.Tracer("carstore").Start(ctx, "writeNewShardFile")
	defer span.End()

	// TODO: some overwrite protections
	fname := filepath.Join(cs.rootDir, fnameForShard(user, seq))
	if err := os.WriteFile(fname, data, 0664); err != nil {
		return "", err
	}

	return fname, nil
}

func (cs *CarStore) deleteShardFile(ctx context.Context, sh *CarShard) error {
	return os.Remove(fnameForShard(sh.Usr, sh.Seq))
}

// CloseWithRoot writes all new blocks in a car file to the writer with the
// given cid as the 'root'
func (ds *DeltaSession) CloseWithRoot(ctx context.Context, root cid.Cid) ([]byte, error) {
	return ds.closeWithRoot(ctx, root, false)
}

func WriteCarHeader(w io.Writer, root cid.Cid) (int64, error) {
	h := &car.CarHeader{
		Roots:   []cid.Cid{root},
		Version: 1,
	}
	hb, err := cbor.DumpObject(h)
	if err != nil {
		return 0, err
	}

	hnw, err := LdWrite(w, hb)
	if err != nil {
		return 0, err
	}

	return hnw, nil
}

func (ds *DeltaSession) closeWithRoot(ctx context.Context, root cid.Cid, rebase bool) ([]byte, error) {
	ctx, span := otel.Tracer("carstore").Start(ctx, "CloseWithRoot")
	defer span.End()

	if ds.readonly {
		return nil, fmt.Errorf("cannot write to readonly deltaSession")
	}

	buf := new(bytes.Buffer)
	hnw, err := WriteCarHeader(buf, root)
	if err != nil {
		return nil, fmt.Errorf("failed to write car header: %w", err)
	}

	// TODO: writing these blocks in map traversal order is bad, I believe the
	// optimal ordering will be something like reverse-write-order, but random
	// is definitely not it

	offset := hnw
	//brefs := make([]*blockRef, 0, len(ds.blks))
	brefs := make([]map[string]interface{}, 0, len(ds.blks))
	for k, blk := range ds.blks {
		nw, err := LdWrite(buf, k.Bytes(), blk.RawData())
		if err != nil {
			return nil, fmt.Errorf("failed to write block: %w", err)
		}

		/*
			brefs = append(brefs, &blockRef{
				Cid:    k.String(),
				Offset: offset,
				Shard:  shard.ID,
			})
		*/
		// adding things to the db by map is the only way to get gorm to not
		// add the 'returning' clause, which costs a lot of time
		brefs = append(brefs, map[string]interface{}{
			"cid":    models.DbCID{k},
			"offset": offset,
		})

		offset += nw
	}

	path, err := ds.cs.writeNewShardFile(ctx, ds.user, ds.seq, buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to write shard file: %w", err)
	}

	// TODO: all this database work needs to be in a single transaction
	shard := CarShard{
		Root:      models.DbCID{root},
		DataStart: hnw,
		Seq:       ds.seq,
		Path:      path,
		Usr:       ds.user,
	}

	if err := ds.putShard(ctx, &shard, brefs); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (ds *DeltaSession) putShard(ctx context.Context, shard *CarShard, brefs []map[string]any) error {
	ctx, span := otel.Tracer("carstore").Start(ctx, "putShard")
	defer span.End()

	// TODO: there should be a way to create the shard and block_refs that
	// reference it in the same query, would save a lot of time
	tx := ds.cs.meta.WithContext(ctx).Begin()

	if err := tx.WithContext(ctx).Create(shard).Error; err != nil {
		return fmt.Errorf("failed to create shard in DB tx: %w", err)
	}
	ds.cs.putLastShardCache(ds.user, shard)

	for _, ref := range brefs {
		ref["shard"] = shard.ID
	}

	if err := createBlockRefs(ctx, tx, brefs); err != nil {
		return fmt.Errorf("failed to create block refs: %w", err)
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

	if err := createInBatches(ctx, tx, brefs, 100); err != nil {
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
		end := i + batchSize
		if end > len(data) {
			end = len(data)
		}

		batch := data[i:end]
		query, values := generateInsertQuery(batch)

		if err := tx.WithContext(ctx).Exec(query, values...).Error; err != nil {
			return err
		}
	}
	return nil
}

func (ds *DeltaSession) CloseAsRebase(ctx context.Context, root cid.Cid) error {
	_, err := ds.closeWithRoot(ctx, root, true)
	if err != nil {
		return err
	}

	// TODO: this *could* get large, might be worth doing it incrementally
	var oldslices []CarShard
	if err := ds.cs.meta.Find(&oldslices, "usr = ? AND seq < ?", ds.user, ds.seq).Error; err != nil {
		return err
	}

	// If anything here fails, cleanup is straightforward. Simply look for any
	// shard in the database with a higher seq shard marked as 'rebase'
	for _, sl := range oldslices {
		if err := os.Remove(sl.Path); err != nil {
			if !os.IsNotExist(err) {
				return err
			}
		}

		if err := ds.cs.meta.Delete(&sl).Error; err != nil {
			return err
		}

		if err := ds.cs.meta.Where("shard = ?", sl.ID).Delete(&blockRef{}).Error; err != nil {
			return err
		}
	}

	return nil
}

func LdWrite(w io.Writer, d ...[]byte) (int64, error) {
	var sum uint64
	for _, s := range d {
		sum += uint64(len(s))
	}

	buf := make([]byte, 8)
	n := binary.PutUvarint(buf, sum)
	nw, err := w.Write(buf[:n])
	if err != nil {
		return 0, err
	}

	for _, s := range d {
		onw, err := w.Write(s)
		if err != nil {
			return int64(nw), err
		}
		nw += onw
	}

	return int64(nw), nil
}

func (cs *CarStore) ImportSlice(ctx context.Context, uid models.Uid, prev *cid.Cid, carslice []byte) (cid.Cid, *DeltaSession, error) {
	ctx, span := otel.Tracer("carstore").Start(ctx, "ImportSlice")
	defer span.End()

	carr, err := car.NewCarReader(bytes.NewReader(carslice))
	if err != nil {
		return cid.Undef, nil, err
	}

	if len(carr.Header.Roots) != 1 {
		return cid.Undef, nil, fmt.Errorf("invalid car file, header must have a single root (has %d)", len(carr.Header.Roots))
	}

	ds, err := cs.NewDeltaSession(ctx, uid, prev)
	if err != nil {
		return cid.Undef, nil, err
	}

	for {
		blk, err := carr.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return cid.Undef, nil, err
		}

		if err := ds.Put(ctx, blk); err != nil {
			return cid.Undef, nil, err
		}
	}

	return carr.Header.Roots[0], ds, nil
}

func (cs *CarStore) GetUserRepoHead(ctx context.Context, user models.Uid) (cid.Cid, error) {
	lastShard, err := cs.getLastShard(ctx, user)
	if err != nil {
		return cid.Undef, err
	}
	if lastShard.ID == 0 {
		return cid.Undef, nil
	}

	return lastShard.Root.CID, nil
}

type UserStat struct {
	Seq     int
	Root    string
	Created time.Time
}

func (cs *CarStore) Stat(ctx context.Context, usr models.Uid) ([]UserStat, error) {
	var shards []CarShard
	if err := cs.meta.Order("seq asc").Find(&shards, "usr = ?", usr).Error; err != nil {
		return nil, err
	}

	var out []UserStat
	for _, s := range shards {
		out = append(out, UserStat{
			Seq:     s.Seq,
			Root:    s.Root.CID.String(),
			Created: s.CreatedAt,
		})
	}

	return out, nil
}

func (cs *CarStore) checkFork(ctx context.Context, user models.Uid, prev cid.Cid) (bool, error) {
	ctx, span := otel.Tracer("carstore").Start(ctx, "checkFork")
	defer span.End()

	lastShard, err := cs.getLastShard(ctx, user)
	if err != nil {
		return false, err
	}

	var maybeShard CarShard
	if err := cs.meta.WithContext(ctx).Model(CarShard{}).Find(&maybeShard, "usr = ? AND root = ?", user, &models.DbCID{prev}).Error; err != nil {
		return false, err
	}

	if maybeShard.ID != 0 && maybeShard.ID == lastShard.ID {
		// somehow we are checking if a valid 'append' is a fork, seems buggy, throw an error
		return false, fmt.Errorf("invariant broken: checked for forkiness of a valid append (%d - %d)", lastShard.ID, maybeShard.ID)
	}

	if maybeShard.ID == 0 {
		return false, nil
	}

	return true, nil
}

func (cs *CarStore) TakeDownRepo(ctx context.Context, user models.Uid) error {
	var shards []CarShard
	if err := cs.meta.Find(&shards, "usr = ?", user).Error; err != nil {
		return err
	}

	for _, sh := range shards {
		if err := cs.deleteShardFile(ctx, &sh); err != nil {
			if !os.IsNotExist(err) {
				return err
			}
		}
	}

	if err := cs.meta.Delete(&CarShard{}, "usr = ?", user).Error; err != nil {
		return err
	}

	return nil
}
