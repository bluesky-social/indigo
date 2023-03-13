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
	"sync"

	util "github.com/bluesky-social/indigo/util"

	blocks "github.com/ipfs/go-block-format"
	carutil "github.com/ipfs/go-car/util"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	cbor "github.com/ipfs/go-ipld-cbor"
	ipld "github.com/ipfs/go-ipld-format"
	car "github.com/ipld/go-car"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

const MaxSliceLength = 2 << 20

type CarStore struct {
	meta    *gorm.DB
	rootDir string

	lscLk          sync.Mutex
	lastShardCache map[util.Uid]*CarShard
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
		lastShardCache: make(map[util.Uid]*CarShard),
	}, nil
}

type UserInfo struct {
	gorm.Model
	Head string
}

type CarShard struct {
	gorm.Model

	Root      util.DbCID
	DataStart int64
	Seq       int `gorm:"index"`
	Path      string
	Usr       util.Uid `gorm:"index"`
}

type blockRef struct {
	ID     uint       `gorm:"primarykey"`
	Cid    util.DbCID `gorm:"index"`
	Shard  uint
	Offset int64
	//User   uint `gorm:"index"`
}

type userView struct {
	cs   *CarStore
	user util.Uid

	cache    map[cid.Cid]blocks.Block
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
		Where("usr = ? AND cid = ?", uv.user, util.DbCID{k}).
		Count(&count).Error; err != nil {
		return false, err
	}

	return count > 0, nil
}

func (uv *userView) Get(ctx context.Context, k cid.Cid) (blocks.Block, error) {
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
		Where("usr = ? AND cid = ?", uv.user, util.DbCID{k}).
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

func (uv *userView) prefetchRead(ctx context.Context, k cid.Cid, path string, offset int64) (blocks.Block, error) {
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

func (uv *userView) singleRead(ctx context.Context, k cid.Cid, path string, offset int64) (blocks.Block, error) {
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

func (uv *userView) Put(ctx context.Context, blk blocks.Block) error {
	return fmt.Errorf("puts not supported to car view blockstores")
}

func (uv *userView) PutMany(ctx context.Context, blks []blocks.Block) error {
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
	blks     map[cid.Cid]blocks.Block
	base     blockstore.Blockstore
	user     util.Uid
	seq      int
	readonly bool
	cs       *CarStore
}

func (cs *CarStore) checkLastShardCache(user util.Uid) *CarShard {
	cs.lscLk.Lock()
	defer cs.lscLk.Unlock()

	ls, ok := cs.lastShardCache[user]
	if ok {
		return ls
	}

	return nil
}

func (cs *CarStore) putLastShardCache(user util.Uid, ls *CarShard) {
	cs.lscLk.Lock()
	defer cs.lscLk.Unlock()

	cs.lastShardCache[user] = ls
}

func (cs *CarStore) getLastShard(ctx context.Context, user util.Uid) (*CarShard, error) {
	maybeLs := cs.checkLastShardCache(user)
	if maybeLs != nil {
		return maybeLs, nil
	}

	var lastShard CarShard
	if err := cs.meta.WithContext(ctx).Model(CarShard{}).Limit(1).Order("id desc").Find(&lastShard, "usr = ?", user).Error; err != nil {
		//if err := cs.meta.Model(CarShard{}).Where("user = ?", user).Last(&lastShard).Error; err != nil {
		//if err != gorm.ErrRecordNotFound {
		return nil, err
		//}
	}

	cs.putLastShardCache(user, &lastShard)
	return &lastShard, nil
}

var ErrRepoBaseMismatch = fmt.Errorf("attempted a delta session on top of the wrong previous head")

func (cs *CarStore) NewDeltaSession(ctx context.Context, user util.Uid, prev *cid.Cid) (*DeltaSession, error) {
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
			return nil, fmt.Errorf("mismatch: %s != %s: %w", lastShard.Root, prev.String(), ErrRepoBaseMismatch)
		}
	}

	return &DeltaSession{
		fresh: blockstore.NewBlockstore(datastore.NewMapDatastore()),
		blks:  make(map[cid.Cid]blocks.Block),
		base: &userView{
			user:     user,
			cs:       cs,
			prefetch: true,
			cache:    make(map[cid.Cid]blocks.Block),
		},
		user: user,
		cs:   cs,
		seq:  lastShard.Seq + 1,
	}, nil
}

func (cs *CarStore) ReadOnlySession(user util.Uid) (*DeltaSession, error) {
	return &DeltaSession{
		base: &userView{
			user:     user,
			cs:       cs,
			prefetch: false,
			cache:    make(map[cid.Cid]blocks.Block),
		},
		readonly: true,
		user:     user,
		cs:       cs,
	}, nil
}

func (cs *CarStore) ReadUserCar(ctx context.Context, user util.Uid, earlyCid, lateCid cid.Cid, incremental bool, w io.Writer) error {
	ctx, span := otel.Tracer("carstore").Start(ctx, "ReadUserCar")
	defer span.End()

	var lateSeq, earlySeq int

	if earlyCid.Defined() {
		var untilShard CarShard
		if err := cs.meta.First(&untilShard, "root = ? AND usr = ?", earlyCid.String(), user).Error; err != nil {
			return err
		}
		earlySeq = untilShard.Seq
	}

	if lateCid.Defined() {
		var fromShard CarShard
		if err := cs.meta.First(&fromShard, "root = ? AND usr = ?", lateCid.String(), user).Error; err != nil {
			return err
		}
		lateSeq = fromShard.Seq
	}

	q := cs.meta.Order("seq desc").Where("usr = ? AND seq >= ?", user, earlySeq)
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

var _ blockstore.Blockstore = (*DeltaSession)(nil)

func (ds *DeltaSession) Put(ctx context.Context, b blocks.Block) error {
	if ds.readonly {
		return fmt.Errorf("cannot write to readonly deltaSession")
	}
	ds.blks[b.Cid()] = b
	return nil
}

func (ds *DeltaSession) PutMany(ctx context.Context, bs []blocks.Block) error {
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

func (ds *DeltaSession) Get(ctx context.Context, c cid.Cid) (blocks.Block, error) {
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

func (cs *CarStore) openNewShardFile(ctx context.Context, user util.Uid, seq int) (*os.File, string, error) {
	// TODO: some overwrite protections
	fname := filepath.Join(cs.rootDir, fmt.Sprintf("sh-%d-%d", user, seq))
	fi, err := os.Create(fname)
	if err != nil {
		return nil, "", err
	}

	return fi, fname, nil
}

func (cs *CarStore) writeNewShardFile(ctx context.Context, user util.Uid, seq int, data []byte) (string, error) {
	// TODO: some overwrite protections
	fname := filepath.Join(cs.rootDir, fmt.Sprintf("sh-%d-%d", user, seq))
	if err := os.WriteFile(fname, data, 0664); err != nil {
		return "", err
	}

	return fname, nil
}

// CloseWithRoot writes all new blocks in a car file to the writer with the
// given cid as the 'root'
func (ds *DeltaSession) CloseWithRoot(ctx context.Context, root cid.Cid) ([]byte, error) {
	ctx, span := otel.Tracer("carstore").Start(ctx, "CloseWithRoot")
	defer span.End()

	if ds.readonly {
		return nil, fmt.Errorf("cannot write to readonly deltaSession")
	}

	buf := new(bytes.Buffer)
	h := &car.CarHeader{
		Roots:   []cid.Cid{root},
		Version: 1,
	}
	hb, err := cbor.DumpObject(h)
	if err != nil {
		return nil, err
	}

	hnw, err := LdWrite(buf, hb)
	if err != nil {
		return nil, err
	}

	// TODO: writing these blocks in map traversal order is bad, I believe the
	// optimal ordering will be something like reverse-write-order, but random
	// is definitely not it
	var offset int64 = hnw
	//brefs := make([]*blockRef, 0, len(ds.blks))
	brefs := make([]map[string]interface{}, 0, len(ds.blks))
	for k, blk := range ds.blks {
		nw, err := LdWrite(buf, k.Bytes(), blk.RawData())
		if err != nil {
			return nil, err
		}

		if buf.Len() > MaxSliceLength {
			return nil, fmt.Errorf("cannot close carstore session, too much data written (%d)", buf.Len())
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
			"cid":    util.DbCID{k},
			"offset": offset,
		})

		offset += nw
	}

	path, err := ds.cs.writeNewShardFile(ctx, ds.user, ds.seq, buf.Bytes())
	if err != nil {
		return nil, err
	}

	// TODO: all this database work needs to be in a single transaction
	shard := CarShard{
		Root:      util.DbCID{root},
		DataStart: hnw,
		Seq:       ds.seq,
		Path:      path,
		Usr:       ds.user,
	}

	// TODO: there should be a way to create the shard and block_refs that
	// reference it in the same query, would save a lot of time
	if err := ds.cs.meta.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&shard).Error; err != nil {
			return err
		}
		ds.cs.putLastShardCache(ds.user, &shard)

		for _, ref := range brefs {
			ref["shard"] = shard.ID
		}

		if err := tx.Table("block_refs").CreateInBatches(brefs, 100).Error; err != nil {
			return err
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
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

func (cs *CarStore) ImportSlice(ctx context.Context, uid util.Uid, prev *cid.Cid, carslice []byte) (cid.Cid, *DeltaSession, error) {

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

func (cs *CarStore) GetUserRepoHead(ctx context.Context, user util.Uid) (cid.Cid, error) {
	lastShard, err := cs.getLastShard(ctx, user)
	if err != nil {
		return cid.Undef, err
	}
	if lastShard.ID == 0 {
		return cid.Undef, nil
	}

	return lastShard.Root.CID, nil
}
