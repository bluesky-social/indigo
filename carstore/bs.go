package carstore

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"

	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-car/util"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	cbor "github.com/ipfs/go-ipld-cbor"
	ipld "github.com/ipfs/go-ipld-format"
	car "github.com/ipld/go-car"
	"gorm.io/gorm"
)

type CarStore struct {
	meta    *gorm.DB
	rootDir string
}

func NewCarStore(meta *gorm.DB, root string) (*CarStore, error) {
	if err := meta.AutoMigrate(&CarShard{}, &blockRef{}); err != nil {
		return nil, err
	}
	return &CarStore{
		meta:    meta,
		rootDir: root,
	}, nil
}

type UserInfo struct {
	gorm.Model
	Head string
}

type CarShard struct {
	gorm.Model

	Root      string
	DataStart int64
	Seq       int
	Path      string
	User      uint
}

type blockRef struct {
	gorm.Model
	Cid    string
	Shard  uint
	Offset int64
}

type userView struct {
	cs   *CarStore
	user uint
}

var _ blockstore.Blockstore = (*userView)(nil)

func (uv *userView) HashOnRead(hor bool) {
	//noop
}

func (uv *userView) Has(ctx context.Context, k cid.Cid) (bool, error) {
	// TODO: for now, im using a join to ensure we only query blocks from the
	// correct user. maybe it makes sense to put the user in the blockRef
	// directly? tradeoff of time vs space
	var count int64
	if err := uv.cs.meta.
		Model(blockRef{}).
		Select("path, offset").
		Joins("left join car_shards on block_refs.shard = car_shards.id").
		Where("user = ? AND cid = ?", uv.user, k.String()).
		Count(&count).Error; err != nil {
		return false, err
	}

	return count > 0, nil
}

func (uv *userView) Get(ctx context.Context, k cid.Cid) (blocks.Block, error) {
	// TODO: for now, im using a join to ensure we only query blocks from the
	// correct user. maybe it makes sense to put the user in the blockRef
	// directly? tradeoff of time vs space
	var info struct {
		Path   string
		Offset int64
	}
	if err := uv.cs.meta.
		Model(blockRef{}).
		Select("path, offset").
		Joins("left join car_shards on block_refs.shard = car_shards.id").
		Where("user = ? AND cid = ?", uv.user, k.String()).
		First(&info).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ipld.ErrNotFound{k}

		}
		return nil, err
	}

	fi, err := os.Open(info.Path)
	if err != nil {
		return nil, err
	}

	seeked, err := fi.Seek(info.Offset, io.SeekStart)
	if err != nil {
		return nil, err
	}

	if seeked != info.Offset {
		return nil, fmt.Errorf("failed to seek to offset (%d != %d)", seeked, info.Offset)
	}

	bufr := bufio.NewReader(fi)
	rcid, data, err := util.ReadNode(bufr)
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
	fresh blockstore.Blockstore
	blks  map[cid.Cid]blocks.Block
	base  blockstore.Blockstore
	user  uint
	seq   int
	cs    *CarStore
}

func (cs *CarStore) NewDeltaSession(user uint, prev cid.Cid) (*DeltaSession, error) {
	// TODO: ensure that we don't write updates on top of the wrong head
	// this needs to be a compare and swap type operation
	var lastShard CarShard
	if err := cs.meta.Model(CarShard{}).Limit(1).Order("seq desc").Find(&lastShard).Error; err != nil {
		return nil, err
	}

	return &DeltaSession{
		fresh: blockstore.NewBlockstore(datastore.NewMapDatastore()),
		blks:  make(map[cid.Cid]blocks.Block),
		base: &userView{
			user: user,
			cs:   cs,
		},
		user: user,
		cs:   cs,
		seq:  lastShard.Seq + 1,
	}, nil
}

func (cs *CarStore) ReadUserCar(ctx context.Context, user uint, until cid.Cid, incremental bool, w io.Writer) error {
	var untilSeq int

	if until.Defined() {
		var untilShard CarShard
		if err := cs.meta.First(&untilShard, "root = ? AND user = ?", until.String(), user).Error; err != nil {
			return err
		}
		untilSeq = untilShard.Seq
	}

	var shards []CarShard
	if err := cs.meta.Order("seq desc").Find(&shards, "user = ? AND seq >= ?", user, untilSeq).Error; err != nil {
		return err
	}

	if !incremental && until.Defined() {
		// have to do it the ugly way
		return fmt.Errorf("nyi")
	}

	if len(shards) == 0 {
		return fmt.Errorf("no data found for user %d", user)
	}

	// fast path!
	rootcid, err := cid.Decode(shards[0].Root)
	if err != nil {
		return err
	}

	if err := car.WriteHeader(&car.CarHeader{
		Roots:   []cid.Cid{rootcid},
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
	fi, err := os.Open(sh.Path)
	if err != nil {
		return err
	}

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
	ds.blks[b.Cid()] = b
	return nil
}

func (ds *DeltaSession) PutMany(ctx context.Context, bs []blocks.Block) error {
	for _, b := range bs {
		ds.blks[b.Cid()] = b
	}
	return nil
}

func (ds *DeltaSession) AllKeysChan(ctx context.Context) (<-chan cid.Cid, error) {
	return nil, fmt.Errorf("AllKeysChan not implemented")
}

func (ds *DeltaSession) DeleteBlock(ctx context.Context, c cid.Cid) error {
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

func (cs *CarStore) openNewShardFile(ctx context.Context, user uint, seq int) (*os.File, string, error) {
	// TODO: some overwrite protections
	fname := filepath.Join(cs.rootDir, fmt.Sprintf("sh-%d-%d", user, seq))
	fi, err := os.Create(fname)
	if err != nil {
		return nil, "", err
	}

	return fi, fname, nil
}

// CloseWithRoot writes all new blocks in a car file to the writer with the
// given cid as the 'root'
func (ds *DeltaSession) CloseWithRoot(ctx context.Context, root cid.Cid) error {
	fi, path, err := ds.cs.openNewShardFile(ctx, ds.user, ds.seq)
	if err != nil {
		return err
	}
	// TODO: if this method fails delete the file
	defer fi.Close()

	h := &car.CarHeader{
		Roots:   []cid.Cid{root},
		Version: 1,
	}
	hb, err := cbor.DumpObject(h)
	if err != nil {
		return err
	}

	hnw, err := LdWrite(fi, hb)
	if err != nil {
		return err
	}

	// TODO: all this database work needs to be in a single transaction
	shard := CarShard{
		Root:      root.String(),
		DataStart: hnw,
		Seq:       ds.seq,
		Path:      path,
		User:      ds.user,
	}

	if err := ds.cs.meta.Create(&shard).Error; err != nil {
		return err
	}

	var offset int64 = hnw
	for k, blk := range ds.blks {
		nw, err := LdWrite(fi, k.Bytes(), blk.RawData())
		if err != nil {
			return err
		}

		if err := ds.cs.meta.Create(&blockRef{
			Cid:    k.String(),
			Offset: offset,
			Shard:  shard.ID,
		}).Error; err != nil {
			return err
		}

		offset += nw
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
