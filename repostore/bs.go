package repostore

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	carstore "github.com/bluesky-social/indigo/carstore"
	"github.com/bluesky-social/indigo/models"

	blockformat "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	cbor "github.com/ipfs/go-ipld-cbor"
	ipld "github.com/ipfs/go-ipld-format"
	"github.com/ipfs/go-libipfs/blocks"
	car "github.com/ipld/go-car"
	carutil "github.com/ipld/go-car/util"
	cbg "github.com/whyrusleeping/cbor-gen"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"gorm.io/gorm"
)

type FileRepoStore struct {
	meta     *CarStoreGormMeta
	rootDirs []string

	lastShardCache lastShardCache

	log *slog.Logger
}

func NewRepoStore(meta *gorm.DB, roots []string) (carstore.CarStore, error) {
	for _, root := range roots {
		if _, err := os.Stat(root); err != nil {
			if !os.IsNotExist(err) {
				return nil, err
			}

			if err := os.Mkdir(root, 0775); err != nil {
				return nil, err
			}
		}
	}
	if err := meta.AutoMigrate(&CarShard{}, &blockRef{}); err != nil {
		return nil, err
	}
	if err := meta.AutoMigrate(&staleRef{}); err != nil {
		return nil, err
	}

	gormMeta := &CarStoreGormMeta{meta: meta}
	out := &FileRepoStore{
		meta:     gormMeta,
		rootDirs: roots,
		lastShardCache: lastShardCache{
			source: gormMeta,
		},
		log: slog.Default().With("system", "carstore"),
	}
	out.lastShardCache.Init()
	return out, nil
}

// wrapper into a block store that keeps track of which user we are working on behalf of
type userView struct {
	user models.Uid

	cache    map[cid.Cid]blockformat.Block
	prefetch bool
}

var _ blockstore.Blockstore = (*userView)(nil)

func (uv *userView) HashOnRead(hor bool) {
	//noop
}

func (uv *userView) Has(ctx context.Context, k cid.Cid) (bool, error) {
	_, have := uv.cache[k]
	if have {
		return have, nil
	}
	return false, nil
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

	return nil, fmt.Errorf("cant do arbitrary reads from this")
}

const prefetchThreshold = 512 << 10

func (uv *userView) prefetchRead(ctx context.Context, k cid.Cid, path string, offset int64) (blockformat.Block, error) {
	ctx, span := otel.Tracer("carstore").Start(ctx, "getLastShard")
	defer span.End()

	fi, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fi.Close()

	st, err := fi.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat file for prefetch: %w", err)
	}

	span.SetAttributes(attribute.Int64("shard_size", st.Size()))

	if st.Size() > prefetchThreshold {
		span.SetAttributes(attribute.Bool("no_prefetch", true))
		return doBlockRead(fi, k, offset)
	}

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

func (uv *userView) preloadBlocksFromFile(ctx context.Context, path string) error {
	fi, err := os.Open(path)
	if err != nil {
		return err
	}
	defer fi.Close()

	cr, err := car.NewCarReader(fi)
	if err != nil {
		return err
	}

	for {
		blk, err := cr.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		uv.cache[blk.Cid()] = blk
	}

	return nil
}

func (uv *userView) singleRead(ctx context.Context, k cid.Cid, path string, offset int64) (blockformat.Block, error) {
	fi, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fi.Close()

	return doBlockRead(fi, k, offset)
}

func doBlockRead(fi *os.File, k cid.Cid, offset int64) (blockformat.Block, error) {
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

// subset of blockstore.Blockstore that we actually use here
type minBlockstore interface {
	Get(ctx context.Context, bcid cid.Cid) (blockformat.Block, error)
	Has(ctx context.Context, bcid cid.Cid) (bool, error)
	GetSize(ctx context.Context, bcid cid.Cid) (int, error)
}

type DeltaSession struct {
	blks     map[cid.Cid]blockformat.Block
	rmcids   map[cid.Cid]bool
	base     minBlockstore
	user     models.Uid
	baseCid  cid.Cid
	seq      int
	readonly bool
	cs       shardWriter
	lastRev  string
}

func (cs *FileRepoStore) checkLastShardCache(user models.Uid) *CarShard {
	return cs.lastShardCache.check(user)
}

func (cs *FileRepoStore) removeLastShardCache(user models.Uid) {
	cs.lastShardCache.remove(user)
}

func (cs *FileRepoStore) putLastShardCache(ls *CarShard) {
	cs.lastShardCache.put(ls)
}

func (cs *FileRepoStore) getLastShard(ctx context.Context, user models.Uid) (*CarShard, error) {
	return cs.lastShardCache.get(ctx, user)
}

func (cs *FileRepoStore) NewDeltaSession(ctx context.Context, user models.Uid, since *string) (carstore.BlockStorage, error) {
	ctx, span := otel.Tracer("carstore2").Start(ctx, "NewSession")
	defer span.End()

	// TODO: ensure that we don't write updates on top of the wrong head
	// this needs to be a compare and swap type operation
	lastShard, err := cs.getLastShard(ctx, user)
	if err != nil {
		return nil, err
	}

	if since != nil && *since != lastShard.Rev {
		return nil, fmt.Errorf("revision mismatch: %s != %s: %w", *since, lastShard.Rev, carstore.ErrRepoBaseMismatch)
	}
	uv := &userView{
		user:     user,
		prefetch: true,
		cache:    make(map[cid.Cid]blockformat.Block),
	}

	if lastShard.ID != 0 {
		if err := uv.preloadBlocksFromFile(ctx, lastShard.Path); err != nil {
			return nil, fmt.Errorf("block prefetch failed: %w", err)
		}
	}

	return &DeltaSession{
		blks:    make(map[cid.Cid]blockformat.Block),
		base:    uv,
		user:    user,
		baseCid: lastShard.Root.CID,
		cs:      cs,
		seq:     lastShard.Seq + 1,
		lastRev: lastShard.Rev,
	}, nil
}

func (cs *FileRepoStore) ReadOnlySession(user models.Uid) (carstore.BlockStorage, error) {
	return &DeltaSession{
		base: &userView{
			user:     user,
			prefetch: false,
			cache:    make(map[cid.Cid]blockformat.Block),
		},
		readonly: true,
		user:     user,
		cs:       cs,
	}, nil
}

// TODO: incremental is only ever called true, remove the param
func (cs *FileRepoStore) ReadUserCar(ctx context.Context, user models.Uid, sinceRev string, incremental bool, shardOut io.Writer) error {
	ctx, span := otel.Tracer("carstore").Start(ctx, "ReadUserCar")
	defer span.End()

	var earlySeq int
	if sinceRev != "" {
		var err error
		earlySeq, err = cs.meta.SeqForRev(ctx, user, sinceRev)
		if err != nil {
			return err
		}
	}

	shards, err := cs.meta.GetUserShardsDesc(ctx, user, earlySeq)
	if err != nil {
		return err
	}

	// TODO: incremental is only ever called true, so this is fine and we can remove the error check
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
	}, shardOut); err != nil {
		return err
	}

	for _, sh := range shards {
		if err := cs.writeShardBlocks(ctx, &sh, shardOut); err != nil {
			return err
		}
	}

	return nil
}

// inner loop part of ReadUserCar
// copy shard blocks from disk to Writer
func (cs *FileRepoStore) writeShardBlocks(ctx context.Context, sh *CarShard, shardOut io.Writer) error {
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

	_, err = io.Copy(shardOut, fi)
	if err != nil {
		return err
	}

	return nil
}

// inner loop part of compactBucket
func (cs *FileRepoStore) iterateShardBlocks(ctx context.Context, sh *CarShard, cb func(blk blockformat.Block) error) error {
	fi, err := os.Open(sh.Path)
	if err != nil {
		return err
	}
	defer fi.Close()

	rr, err := car.NewCarReader(fi)
	if err != nil {
		return fmt.Errorf("opening shard car: %w", err)
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
		return ipld.ErrNotFound{Cid: c}
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

func (cs *FileRepoStore) dirForUser(user models.Uid) string {
	return cs.rootDirs[int(user)%len(cs.rootDirs)]
}

func (cs *FileRepoStore) openNewShardFile(ctx context.Context, user models.Uid, seq int) (*os.File, string, error) {
	// TODO: some overwrite protections
	fname := filepath.Join(cs.dirForUser(user), fnameForShard(user, seq))
	fi, err := os.Create(fname)
	if err != nil {
		return nil, "", err
	}

	return fi, fname, nil
}

func (cs *FileRepoStore) writeNewShardFile(ctx context.Context, user models.Uid, seq int, data []byte) (string, error) {
	_, span := otel.Tracer("carstore").Start(ctx, "writeNewShardFile")
	defer span.End()

	// TODO: some overwrite protections
	fname := filepath.Join(cs.dirForUser(user), fnameForShard(user, seq))
	if err := os.WriteFile(fname, data, 0664); err != nil {
		return "", err
	}

	return fname, nil
}

func (cs *FileRepoStore) deleteShardFile(ctx context.Context, sh *CarShard) error {
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

	hnw, err := carstore.LdWrite(w, hb)
	if err != nil {
		return 0, err
	}

	return hnw, nil
}

// shardWriter.writeNewShard called from inside DeltaSession.CloseWithRoot
type shardWriter interface {
	// writeNewShard stores blocks in `blks` arg and creates a new shard to propagate out to our firehose
	writeNewShard(ctx context.Context, root cid.Cid, rev string, user models.Uid, seq int, blks map[cid.Cid]blockformat.Block, rmcids map[cid.Cid]bool) ([]byte, error)
}

func blocksToCar(ctx context.Context, root cid.Cid, rev string, blks map[cid.Cid]blockformat.Block) ([]byte, error) {
	buf := new(bytes.Buffer)
	_, err := WriteCarHeader(buf, root)
	if err != nil {
		return nil, fmt.Errorf("failed to write car header: %w", err)
	}

	for k, blk := range blks {
		_, err := carstore.LdWrite(buf, k.Bytes(), blk.RawData())
		if err != nil {
			return nil, fmt.Errorf("failed to write block: %w", err)
		}
	}

	return buf.Bytes(), nil
}

func (cs *FileRepoStore) writeNewShard(ctx context.Context, root cid.Cid, rev string, user models.Uid, seq int, blks map[cid.Cid]blockformat.Block, rmcids map[cid.Cid]bool) ([]byte, error) {

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
		nw, err := carstore.LdWrite(buf, k.Bytes(), blk.RawData())
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
			"cid":    models.DbCID{CID: k},
			"offset": offset,
		})

		offset += nw
	}

	start := time.Now()
	path, err := cs.writeNewShardFile(ctx, user, seq, buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to write shard file: %w", err)
	}
	writeShardFileDuration.Observe(time.Since(start).Seconds())

	shard := CarShard{
		Root:      models.DbCID{CID: root},
		DataStart: hnw,
		Seq:       seq,
		Path:      path,
		Usr:       user,
		Rev:       rev,
	}

	start = time.Now()
	if err := cs.putShard(ctx, &shard, brefs, rmcids, false); err != nil {
		return nil, err
	}
	writeShardMetadataDuration.Observe(time.Since(start).Seconds())

	return buf.Bytes(), nil
}

func (cs *FileRepoStore) putShard(ctx context.Context, shard *CarShard, brefs []map[string]any, rmcids map[cid.Cid]bool, nocache bool) error {
	ctx, span := otel.Tracer("carstore").Start(ctx, "putShard")
	defer span.End()

	err := cs.meta.PutShardAndRefs(ctx, shard, brefs, rmcids)
	if err != nil {
		return err
	}

	if !nocache {
		cs.putLastShardCache(shard)
	}

	return nil
}

func BlockDiff(ctx context.Context, bs blockstore.Blockstore, oldroot cid.Cid, newcids map[cid.Cid]blockformat.Block, skipcids map[cid.Cid]bool) (map[cid.Cid]bool, error) {
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
			return nil, fmt.Errorf("get failed in new tree: %w", err)
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

		if skipcids != nil && skipcids[c] {
			continue
		}

		oblk, err := bs.Get(ctx, c)
		if err != nil {
			return nil, fmt.Errorf("get failed in old tree: %w", err)
		}

		if err := cbg.ScanForLinks(bytes.NewReader(oblk.RawData()), func(lnk cid.Cid) {
			if lnk.Prefix().Codec != cid.DagCBOR {
				return
			}

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

func (cs *FileRepoStore) ImportSlice(ctx context.Context, uid models.Uid, since *string, carslice []byte) (cid.Cid, carstore.BlockStorage, error) {
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

func (ds *DeltaSession) CalcDiff(ctx context.Context, skipcids map[cid.Cid]bool) error {
	rmcids, err := BlockDiff(ctx, ds, ds.baseCid, ds.blks, skipcids)
	if err != nil {
		return fmt.Errorf("block diff failed (base=%s,rev=%s): %w", ds.baseCid, ds.lastRev, err)
	}

	ds.rmcids = rmcids
	return nil
}

func (cs *FileRepoStore) GetUserRepoHead(ctx context.Context, user models.Uid) (cid.Cid, error) {
	lastShard, err := cs.getLastShard(ctx, user)
	if err != nil {
		return cid.Undef, err
	}
	if lastShard.ID == 0 {
		return cid.Undef, nil
	}

	return lastShard.Root.CID, nil
}

func (cs *FileRepoStore) GetUserRepoRev(ctx context.Context, user models.Uid) (string, error) {
	lastShard, err := cs.getLastShard(ctx, user)
	if err != nil {
		return "", err
	}
	if lastShard.ID == 0 {
		return "", nil
	}

	return lastShard.Rev, nil
}

func (cs *FileRepoStore) Stat(ctx context.Context, usr models.Uid) ([]carstore.UserStat, error) {
	shards, err := cs.meta.GetUserShards(ctx, usr)
	if err != nil {
		return nil, err
	}

	var out []carstore.UserStat
	for _, s := range shards {
		out = append(out, carstore.UserStat{
			Seq:     s.Seq,
			Root:    s.Root.CID.String(),
			Created: s.CreatedAt,
		})
	}

	return out, nil
}

func (cs *FileRepoStore) WipeUserData(ctx context.Context, user models.Uid) error {
	shards, err := cs.meta.GetUserShards(ctx, user)
	if err != nil {
		return err
	}

	if err := cs.deleteShards(ctx, shards); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
	}

	cs.removeLastShardCache(user)

	return nil
}

func (cs *FileRepoStore) deleteShards(ctx context.Context, shs []CarShard) error {
	ctx, span := otel.Tracer("carstore").Start(ctx, "deleteShards")
	defer span.End()

	deleteSlice := func(ctx context.Context, subs []CarShard) error {
		ids := make([]uint, len(subs))
		for i, sh := range subs {
			ids[i] = sh.ID
		}

		err := cs.meta.DeleteShardsAndRefs(ctx, ids)
		if err != nil {
			return err
		}

		for _, sh := range subs {
			if err := cs.deleteShardFile(ctx, &sh); err != nil {
				if !os.IsNotExist(err) {
					return err
				}
				cs.log.Warn("shard file we tried to delete did not exist", "shard", sh.ID, "path", sh.Path)
			}
		}

		return nil
	}

	chunkSize := 2000
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

type shardStat struct {
	ID    uint
	Dirty int
	Seq   int
	Total int

	refs []blockRef
}

func (s shardStat) dirtyFrac() float64 {
	return float64(s.Dirty) / float64(s.Total)
}

func aggrRefs(brefs []blockRef, shards map[uint]CarShard, staleCids map[cid.Cid]bool) []shardStat {
	byId := make(map[uint]*shardStat)

	for _, br := range brefs {
		s, ok := byId[br.Shard]
		if !ok {
			s = &shardStat{
				ID:  br.Shard,
				Seq: shards[br.Shard].Seq,
			}
			byId[br.Shard] = s
		}

		s.Total++
		if staleCids[br.Cid.CID] {
			s.Dirty++
		}

		s.refs = append(s.refs, br)
	}

	var out []shardStat
	for _, s := range byId {
		out = append(out, *s)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Seq < out[j].Seq
	})

	return out
}

type compBucket struct {
	shards []shardStat

	cleanBlocks int
	expSize     int
}

func (cb *compBucket) shouldCompact() bool {
	if len(cb.shards) == 0 {
		return false
	}

	if len(cb.shards) > 5 {
		return true
	}

	var frac float64
	for _, s := range cb.shards {
		frac += s.dirtyFrac()
	}
	frac /= float64(len(cb.shards))

	if len(cb.shards) > 3 && frac > 0.2 {
		return true
	}

	return frac > 0.4
}

func (cb *compBucket) addShardStat(ss shardStat) {
	cb.cleanBlocks += (ss.Total - ss.Dirty)
	cb.shards = append(cb.shards, ss)
}

func (cb *compBucket) isEmpty() bool {
	return len(cb.shards) == 0
}

func (cs *FileRepoStore) openNewCompactedShardFile(ctx context.Context, user models.Uid, seq int) (*os.File, string, error) {
	// TODO: some overwrite protections
	// NOTE CreateTemp is used for creating a non-colliding file, but we keep it and don't delete it so don't think of it as "temporary".
	// This creates "sh-%d-%d%s" with some random stuff in the last position
	fi, err := os.CreateTemp(cs.dirForUser(user), fnameForShard(user, seq))
	if err != nil {
		return nil, "", err
	}

	return fi, fi.Name(), nil
}

type CompactionTarget struct {
	Usr       models.Uid
	NumShards int
}

func (cs *FileRepoStore) GetCompactionTargets(ctx context.Context, shardCount int) ([]carstore.CompactionTarget, error) {
	ctx, span := otel.Tracer("carstore").Start(ctx, "GetCompactionTargets")
	defer span.End()

	return cs.meta.GetCompactionTargets(ctx, shardCount)
}

func shardSize(sh *CarShard) (int64, error) {
	st, err := os.Stat(sh.Path)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Warn("missing shard, return size of zero", "path", sh.Path, "shard", sh.ID, "system", "carstore")
			return 0, nil
		}
		return 0, fmt.Errorf("stat %q: %w", sh.Path, err)
	}

	return st.Size(), nil
}

func (cs *FileRepoStore) CompactUserShards(ctx context.Context, user models.Uid, skipBigShards bool) (*carstore.CompactionStats, error) {
	ctx, span := otel.Tracer("repostore").Start(ctx, "CompactUserShards")
	defer span.End()

	span.SetAttributes(attribute.Int64("user", int64(user)))

	shards, err := cs.meta.GetUserShards(ctx, user)
	if err != nil {
		return nil, err
	}

	_ = shards
	return nil, fmt.Errorf("TODO: have to redo all of compaction")
}

func (cs *FileRepoStore) deleteStaleRefs(ctx context.Context, uid models.Uid, brefs []blockRef, staleRefs []staleRef, removedShards map[uint]bool) error {
	ctx, span := otel.Tracer("repostore").Start(ctx, "deleteStaleRefs")
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

	return cs.meta.SetStaleRef(ctx, uid, staleToKeep)
}

func (cs *FileRepoStore) compactBucket(ctx context.Context, user models.Uid, b *compBucket, shardsById map[uint]CarShard, keep map[cid.Cid]bool) error {
	ctx, span := otel.Tracer("repostore").Start(ctx, "compactBucket")
	defer span.End()

	span.SetAttributes(attribute.Int("shards", len(b.shards)))

	last := b.shards[len(b.shards)-1]
	lastsh := shardsById[last.ID]
	fi, path, err := cs.openNewCompactedShardFile(ctx, user, last.Seq)
	if err != nil {
		return fmt.Errorf("opening new file: %w", err)
	}

	defer fi.Close()
	root := lastsh.Root.CID

	hnw, err := WriteCarHeader(fi, root)
	if err != nil {
		return err
	}

	offset := hnw
	var nbrefs []map[string]any
	written := make(map[cid.Cid]bool)
	for _, s := range b.shards {
		sh := shardsById[s.ID]
		if err := cs.iterateShardBlocks(ctx, &sh, func(blk blockformat.Block) error {
			if written[blk.Cid()] {
				return nil
			}

			if keep[blk.Cid()] {
				nw, err := carstore.LdWrite(fi, blk.Cid().Bytes(), blk.RawData())
				if err != nil {
					return fmt.Errorf("failed to write block: %w", err)
				}

				nbrefs = append(nbrefs, map[string]interface{}{
					"cid":    models.DbCID{CID: blk.Cid()},
					"offset": offset,
				})

				offset += nw
				written[blk.Cid()] = true
			}
			return nil
		}); err != nil {
			// If we ever fail to iterate a shard file because its
			// corrupted, just log an error and skip the shard
			cs.log.Error("iterating blocks in shard", "shard", s.ID, "err", err, "uid", user)
		}
	}

	shard := CarShard{
		Root:      models.DbCID{CID: root},
		DataStart: hnw,
		Seq:       lastsh.Seq,
		Path:      path,
		Usr:       user,
		Rev:       lastsh.Rev,
	}

	if err := cs.putShard(ctx, &shard, nbrefs, nil, true); err != nil {
		// if writing the shard fails, we should also delete the file
		_ = fi.Close()

		if err2 := os.Remove(fi.Name()); err2 != nil {
			cs.log.Error("failed to remove shard file after failed db transaction", "path", fi.Name(), "err", err2)
		}

		return err
	}
	return nil
}
