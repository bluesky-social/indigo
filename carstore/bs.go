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
	"sort"
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
	cbg "github.com/whyrusleeping/cbor-gen"
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
	Rev       string
}

type blockRef struct {
	ID     uint         `gorm:"primarykey"`
	Cid    models.DbCID `gorm:"index"`
	Shard  uint
	Offset int64
	Dirty  bool
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
	rmcids   map[cid.Cid]bool
	base     blockstore.Blockstore
	user     models.Uid
	baseCid  cid.Cid
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

func (cs *CarStore) putLastShardCache(ls *CarShard) {
	cs.lscLk.Lock()
	defer cs.lscLk.Unlock()

	cs.lastShardCache[ls.Usr] = ls
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

	cs.putLastShardCache(&lastShard)
	return &lastShard, nil
}

var ErrRepoBaseMismatch = fmt.Errorf("attempted a delta session on top of the wrong previous head")

func (cs *CarStore) NewDeltaSession(ctx context.Context, user models.Uid, since *string) (*DeltaSession, error) {
	ctx, span := otel.Tracer("carstore").Start(ctx, "NewSession")
	defer span.End()

	// TODO: ensure that we don't write updates on top of the wrong head
	// this needs to be a compare and swap type operation
	lastShard, err := cs.getLastShard(ctx, user)
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
			cs:       cs,
			prefetch: true,
			cache:    make(map[cid.Cid]blockformat.Block),
		},
		user:    user,
		baseCid: lastShard.Root.CID,
		cs:      cs,
		seq:     lastShard.Seq + 1,
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

func (cs *CarStore) ReadUserCar(ctx context.Context, user models.Uid, sinceRev string, incremental bool, w io.Writer) error {
	ctx, span := otel.Tracer("carstore").Start(ctx, "ReadUserCar")
	defer span.End()

	var earlySeq int
	if sinceRev != "" {
		var untilShard CarShard
		if err := cs.meta.Where("rev >= ? AND usr = ?", sinceRev, user).Order("rev").First(&untilShard).Error; err != nil {
			return fmt.Errorf("finding early shard: %w", err)
		}
		earlySeq = untilShard.Seq
	}

	var shards []CarShard
	if err := cs.meta.Order("seq desc").Where("usr = ? AND seq >= ?", user, earlySeq).Find(&shards).Error; err != nil {
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
		if err := cs.writeShardBlocks(ctx, &sh, w); err != nil {
			return err
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

func (cs *CarStore) iterateShardBlocks(ctx context.Context, sh *CarShard, cb func(blk blockformat.Block) error) error {
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
			if err == io.EOF {
				return nil
			}
			return err
		}

		if err := cb(blk); err != nil {
			return err
		}
	}
}

var _ blockstore.Blockstore = (*DeltaSession)(nil)

func (ds *DeltaSession) BaseCid() cid.Cid {
	return ds.baseCid
}

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
	return os.Remove(sh.Path)
}

// CloseWithRoot writes all new blocks in a car file to the writer with the
// given cid as the 'root'
func (ds *DeltaSession) CloseWithRoot(ctx context.Context, root cid.Cid, rev string) ([]byte, error) {
	ctx, span := otel.Tracer("carstore").Start(ctx, "CloseWithRoot")
	defer span.End()

	if ds.readonly {
		return nil, fmt.Errorf("cannot write to readonly deltaSession")
	}

	return ds.cs.writeNewShard(ctx, root, rev, ds.user, ds.seq, ds.blks, ds.rmcids)
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

func (cs *CarStore) writeNewShard(ctx context.Context, root cid.Cid, rev string, user models.Uid, seq int, blks map[cid.Cid]blockformat.Block, rmcids map[cid.Cid]bool) ([]byte, error) {

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
	brefs := make([]map[string]interface{}, 0, len(blks))
	for k, blk := range blks {
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

	path, err := cs.writeNewShardFile(ctx, user, seq, buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to write shard file: %w", err)
	}

	shard := CarShard{
		Root:      models.DbCID{root},
		DataStart: hnw,
		Seq:       seq,
		Path:      path,
		Usr:       user,
		Rev:       rev,
	}

	if err := cs.putShard(ctx, &shard, brefs, rmcids, false); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (cs *CarStore) putShard(ctx context.Context, shard *CarShard, brefs []map[string]any, rmcids map[cid.Cid]bool, nocache bool) error {
	ctx, span := otel.Tracer("carstore").Start(ctx, "putShard")
	defer span.End()

	// TODO: there should be a way to create the shard and block_refs that
	// reference it in the same query, would save a lot of time
	tx := cs.meta.WithContext(ctx).Begin()

	if err := tx.WithContext(ctx).Create(shard).Error; err != nil {
		return fmt.Errorf("failed to create shard in DB tx: %w", err)
	}

	if !nocache {
		cs.putLastShardCache(shard)
	}

	for _, ref := range brefs {
		ref["shard"] = shard.ID
	}

	if err := createBlockRefs(ctx, tx, brefs); err != nil {
		return fmt.Errorf("failed to create block refs: %w", err)
	}

	if len(rmcids) > 0 {
		var torm []models.DbCID
		for c := range rmcids {
			torm = append(torm, models.DbCID{c})
		}

		subq := cs.meta.Model(&blockRef{}).Joins("left join car_shards cs on cs.id = block_refs.shard").Where("cid in (?) AND usr = ?", torm, shard.Usr).Select("block_refs.id")
		if err := tx.Model(&blockRef{}).Where("id in (?)", subq).UpdateColumn("dirty", true).Error; err != nil {
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

func setToSlice(s map[cid.Cid]bool) []cid.Cid {
	out := make([]cid.Cid, 0, len(s))
	for c := range s {
		out = append(out, c)
	}

	return out
}

func BlockDiff(ctx context.Context, bs blockstore.Blockstore, oldroot cid.Cid, newcids map[cid.Cid]blockformat.Block) (map[cid.Cid]bool, error) {
	ctx, span := otel.Tracer("repo").Start(ctx, "BlockDiff")
	defer span.End()

	if !oldroot.Defined() {
		return map[cid.Cid]bool{}, nil
	}

	// walk the entire 'new' portion of the tree, marking all referenced cids as 'keep'
	keepset := make(map[cid.Cid]bool)
	for c := range newcids {
		keepset[c] = true
		oblk, err := bs.Get(ctx, c)
		if err != nil {
			return nil, err
		}

		if err := cbg.ScanForLinks(bytes.NewReader(oblk.RawData()), func(lnk cid.Cid) {
			keepset[lnk] = true
		}); err != nil {
			return nil, err
		}
	}

	if keepset[oldroot] {
		// this should probably never happen, but is technically correct
		return nil, nil
	}

	// next, walk the old tree from the root, recursing on cids *not* in the keepset.
	dropset := make(map[cid.Cid]bool)
	dropset[oldroot] = true
	queue := []cid.Cid{oldroot}

	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]

		oblk, err := bs.Get(ctx, c)
		if err != nil {
			return nil, err
		}

		if err := cbg.ScanForLinks(bytes.NewReader(oblk.RawData()), func(lnk cid.Cid) {
			if !keepset[lnk] {
				dropset[lnk] = true
				queue = append(queue, lnk)
			}
		}); err != nil {
			return nil, err
		}
	}

	return dropset, nil
}

func (cs *CarStore) ImportSlice(ctx context.Context, uid models.Uid, since *string, carslice []byte) (cid.Cid, *DeltaSession, error) {
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

	rmcids, err := BlockDiff(ctx, ds, ds.baseCid, ds.blks)
	if err != nil {
		return cid.Undef, nil, fmt.Errorf("block diff failed (base=%s): %w", ds.baseCid, err)
	}

	ds.rmcids = rmcids

	return carr.Header.Roots[0], ds, nil
}

func (ds *DeltaSession) CalcDiff(ctx context.Context, nroot cid.Cid) error {
	rmcids, err := BlockDiff(ctx, ds, ds.baseCid, ds.blks)
	if err != nil {
		return fmt.Errorf("block diff failed: %w", err)
	}

	ds.rmcids = rmcids
	return nil
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

func (cs *CarStore) GetUserRepoRev(ctx context.Context, user models.Uid) (string, error) {
	lastShard, err := cs.getLastShard(ctx, user)
	if err != nil {
		return "", err
	}
	if lastShard.ID == 0 {
		return "", nil
	}

	return lastShard.Rev, nil
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

func (cs *CarStore) TakeDownRepo(ctx context.Context, user models.Uid) error {
	var shards []CarShard
	if err := cs.meta.Find(&shards, "usr = ?", user).Error; err != nil {
		return err
	}

	for _, sh := range shards {
		if err := cs.deleteShard(ctx, &sh); err != nil {
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

func (cs *CarStore) deleteShard(ctx context.Context, sh *CarShard) error {
	if err := cs.meta.Delete(&CarShard{}, "id = ?", sh.ID).Error; err != nil {
		return err
	}

	if err := cs.meta.Delete(&blockRef{}, "shard = ?", sh.ID).Error; err != nil {
		return err
	}

	return cs.deleteShardFile(ctx, sh)
}

type shardStat struct {
	ID    uint
	Seq   int
	Dirty int
	Total int

	refs []blockRef
}

func (s shardStat) dirtyFrac() float64 {
	return float64(s.Dirty) / float64(s.Total)
}

func shouldCompact(s shardStat) bool {
	// if shard is mostly removed blocks
	if s.dirtyFrac() > 0.5 {
		return true
	}

	// if its a big shard with a sufficient number of removed blocks
	if s.Dirty > 1000 {
		return true
	}

	// if its just rather small and we want to compact it up with other shards
	if s.Total < 20 {
		return true
	}

	return false
}

func aggrRefs(brefs []blockRef) []shardStat {
	byId := make(map[uint]*shardStat)

	for _, br := range brefs {
		s, ok := byId[br.Shard]
		if !ok {
			s = &shardStat{
				ID: br.Shard,
			}
			byId[br.Shard] = s
		}

		s.Total++
		if br.Dirty {
			s.Dirty++
		}

		s.refs = append(s.refs, br)
	}

	var out []shardStat
	for _, s := range byId {
		out = append(out, *s)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})

	return out
}

type compBucket struct {
	shards []shardStat

	cleanBlocks int
}

func (cb *compBucket) addShardStat(ss shardStat) {
	cb.cleanBlocks += (ss.Total - ss.Dirty)
	cb.shards = append(cb.shards, ss)
}

func (cb *compBucket) isEmpty() bool {
	return len(cb.shards) == 0
}

func (cs *CarStore) copyShardBlocksFiltered(ctx context.Context, sh *CarShard, w io.Writer, keep map[cid.Cid]bool) error {
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

		if keep[blk.Cid()] {
			_, err := LdWrite(w, blk.Cid().Bytes(), blk.RawData())
			return err
		}
	}
}

func (cs *CarStore) openNewCompactedShardFile(ctx context.Context, user models.Uid, seq int) (*os.File, string, error) {
	// TODO: some overwrite protections
	fi, err := os.CreateTemp(cs.rootDir, fnameForShard(user, seq))
	if err != nil {
		return nil, "", err
	}

	return fi, fi.Name(), nil
}

func (cs *CarStore) CompactUserShards(ctx context.Context, user models.Uid) error {
	/*
			var results []shardStat
			if err := cs.meta.Raw(`
		WITH user_shards AS
			(select cs.id, cs.seq, br.dirty from block_refs br left join car_shards cs on br.shard = cs.id where cs.usr = ?)
		SELECT
			id,
			seq,
			count(*) as total,
			sum(case when dirty then 1 else 0 end) as dirty
		FROM user_shards group by id, seq;`, user).Scan(&results).Error; err != nil {
				return err
			}
	*/

	var brefs []blockRef
	if err := cs.meta.Raw(`select br.* from block_refs br left join car_shards cs on br.shard = cs.id where cs.usr = ?`, user).Scan(&brefs).Error; err != nil {
		return err
	}

	cset := make(map[cid.Cid]bool)
	var hasDirtyDupes bool
	for _, br := range brefs {
		if br.Dirty {
			if cset[br.Cid.CID] {
				hasDirtyDupes = true
				break
			}
			cset[br.Cid.CID] = true
		}
	}

	if hasDirtyDupes {
		// if we have no duplicates, then the keep set is simply all the 'clean' blockRefs
		// in the case we have duplicate dirty references we have to compute
		// the keep set by walking the entire repo to check if anything is
		// still referencing the dirty block in question

		// we could also just add the duplicates to the keep set for now and
		// focus on compacting everything else. it leaves *some* dirty blocks
		// still around but we're doing that anyways since compaction isnt a
		// perfect process
		return fmt.Errorf("WIP: not currently handling this case")
	}

	keep := make(map[cid.Cid]bool)
	for _, br := range brefs {
		if !br.Dirty {
			keep[br.Cid.CID] = true
		}
	}

	results := aggrRefs(brefs)

	var shardIds []uint
	for _, r := range results {
		shardIds = append(shardIds, r.ID)
	}

	var shards []CarShard
	if err := cs.meta.Find(&shards, "id in (?)", shardIds).Error; err != nil {
		return err
	}

	shardsById := make(map[uint]CarShard)
	for _, s := range shards {
		shardsById[s.ID] = s
	}

	thresholdForPosition := func(i int) int {
		// TODO: calculate some curve here so earlier shards end up with more
		// blocks and recent shards end up with less
		return 50
	}

	cur := new(compBucket)
	var compactionQueue []*compBucket
	for i, r := range results {
		if shouldCompact(r) {
			if cur.cleanBlocks > thresholdForPosition(i) {
				compactionQueue = append(compactionQueue, cur)
				cur = new(compBucket)
			}

			cur.addShardStat(r)
		} else {
			if !cur.isEmpty() {
				compactionQueue = append(compactionQueue, cur)
				cur = new(compBucket)
			}
		}
	}

	if !cur.isEmpty() {
		compactionQueue = append(compactionQueue, cur)
	}

	for _, b := range compactionQueue {
		if err := cs.compactBucket(ctx, user, b, shardsById, keep); err != nil {
			return err
		}

		for _, s := range b.shards {
			sh, ok := shardsById[s.ID]
			if !ok {
				return fmt.Errorf("missing shard to delete")
			}

			if err := cs.deleteShard(ctx, &sh); err != nil {
				return fmt.Errorf("deleting shard: %w", err)
			}
		}
	}

	return nil
}

func (cs *CarStore) compactBucket(ctx context.Context, user models.Uid, b *compBucket, shardsById map[uint]CarShard, keep map[cid.Cid]bool) error {
	last := b.shards[len(b.shards)-1]
	lastsh := shardsById[last.ID]
	fi, path, err := cs.openNewCompactedShardFile(ctx, user, last.Seq)
	if err != nil {
		return err
	}

	defer fi.Close()
	root := lastsh.Root.CID

	hnw, err := WriteCarHeader(fi, root)
	if err != nil {
		return err
	}

	offset := hnw
	var nbrefs []map[string]any
	for _, s := range b.shards {
		sh := shardsById[s.ID]
		if err := cs.iterateShardBlocks(ctx, &sh, func(blk blockformat.Block) error {
			if keep[blk.Cid()] {
				nw, err := LdWrite(fi, blk.Cid().Bytes(), blk.RawData())
				if err != nil {
					return fmt.Errorf("failed to write block: %w", err)
				}

				nbrefs = append(nbrefs, map[string]interface{}{
					"cid":    models.DbCID{blk.Cid()},
					"offset": offset,
				})

				offset += nw
			}
			return nil
		}); err != nil {
			return err
		}
	}

	shard := CarShard{
		Root:      models.DbCID{root},
		DataStart: hnw,
		Seq:       lastsh.Seq,
		Path:      path,
		Usr:       user,
		Rev:       lastsh.Rev,
	}

	if err := cs.putShard(ctx, &shard, nbrefs, nil, true); err != nil {
		return err
	}
	return nil
}
