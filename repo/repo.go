package repo

import (
	"context"
	"fmt"
	"io"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/ipld/go-car/v2"
	cbg "github.com/whyrusleeping/cbor-gen"
	"github.com/whyrusleeping/gosky/lex/util"
	"github.com/whyrusleeping/gosky/mst"
	"go.opentelemetry.io/otel"
)

type SignedRoot struct {
	Root cid.Cid `cborgen:"root"`
	Sig  []byte  `cborgen:"sig"`
}

type Commit struct {
	AuthToken *string  `cborgen:"auth_token"`
	Data      cid.Cid  `cborgen:"data"`
	Meta      cid.Cid  `cborgen:"meta"`
	Prev      *cid.Cid `cborgen:"prev,omitempty"`
}

type Meta struct {
	Datastore string `cborgen:"datastore"`
	Did       string `cborgen:"did"`
	Version   int64  `cborgen:"version"`
}

type Repo struct {
	sr  SignedRoot
	cst cbor.IpldStore
	bs  blockstore.Blockstore

	meta Meta

	mst *mst.MerkleSearchTree

	dirty bool
}

func IngestRepo(ctx context.Context, bs blockstore.Blockstore, r io.Reader) (cid.Cid, error) {
	ctx, span := otel.Tracer("repo").Start(ctx, "Ingest")
	defer span.End()

	br, err := car.NewBlockReader(r)
	if err != nil {
		return cid.Undef, err
	}

	for {
		blk, err := br.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return cid.Undef, err
		}

		if err := bs.Put(ctx, blk); err != nil {
			return cid.Undef, err
		}
	}

	return br.Roots[0], nil
}

func ReadRepoFromCar(ctx context.Context, r io.Reader) (*Repo, error) {
	bs := blockstore.NewBlockstore(datastore.NewMapDatastore())
	root, err := IngestRepo(ctx, bs, r)
	if err != nil {
		return nil, err
	}

	return OpenRepo(ctx, bs, root)
}

func NewRepo(ctx context.Context, bs blockstore.Blockstore) *Repo {
	cst := cbor.NewCborStore(bs)

	t := mst.NewMST(cst, 32, cid.Undef, []mst.NodeEntry{}, 0)

	return &Repo{
		cst:   cst,
		bs:    bs,
		mst:   t,
		dirty: true,
	}
}

func OpenRepo(ctx context.Context, bs blockstore.Blockstore, root cid.Cid) (*Repo, error) {
	cst := cbor.NewCborStore(bs)

	var sr SignedRoot
	if err := cst.Get(ctx, root, &sr); err != nil {
		return nil, fmt.Errorf("loading root from blockstore: %w", err)
	}

	return &Repo{
		sr:  sr,
		bs:  bs,
		cst: cst,
	}, nil
}

type CborMarshaler interface {
	MarshalCBOR(w io.Writer) error
}

func (r *Repo) Blockstore() blockstore.Blockstore {
	return r.bs
}

func (r *Repo) CreateRecord(ctx context.Context, nsid string, rec CborMarshaler) (cid.Cid, string, error) {
	ctx, span := otel.Tracer("repo").Start(ctx, "CreateRecord")
	defer span.End()

	r.dirty = true
	t, err := r.getMst(ctx)
	if err != nil {
		return cid.Undef, "", fmt.Errorf("failed to get mst: %w", err)
	}

	k, err := r.cst.Put(ctx, rec)
	if err != nil {
		return cid.Undef, "", err
	}

	tid := NextTID()

	nmst, err := t.Add(ctx, nsid+"/"+tid, k, -1)
	if err != nil {
		return cid.Undef, "", fmt.Errorf("mst.Add failed: %w", err)
	}

	r.mst = nmst
	return k, tid, nil
}

func (r *Repo) PutRecord(ctx context.Context, rpath string, rec CborMarshaler) (cid.Cid, error) {
	ctx, span := otel.Tracer("repo").Start(ctx, "PutRecord")
	defer span.End()

	r.dirty = true
	t, err := r.getMst(ctx)
	if err != nil {
		return cid.Undef, fmt.Errorf("failed to get mst: %w", err)
	}

	k, err := r.cst.Put(ctx, rec)
	if err != nil {
		return cid.Undef, err
	}

	nmst, err := t.Add(ctx, rpath, k, -1)
	if err != nil {
		return cid.Undef, fmt.Errorf("mst.Add failed: %w", err)
	}

	r.mst = nmst
	return k, nil
}

func (r *Repo) DeleteRecord(ctx context.Context, rpath string) error {
	ctx, span := otel.Tracer("repo").Start(ctx, "DeleteRecord")
	defer span.End()

	r.dirty = true
	t, err := r.getMst(ctx)
	if err != nil {
		return fmt.Errorf("failed to get mst: %w", err)
	}

	nmst, err := t.Delete(ctx, rpath)
	if err != nil {
		return fmt.Errorf("mst.Add failed: %w", err)
	}

	r.mst = nmst
	return nil
}

func (r *Repo) Commit(ctx context.Context) (cid.Cid, error) {
	ctx, span := otel.Tracer("repo").Start(ctx, "Commit")
	defer span.End()

	t, err := r.getMst(ctx)
	if err != nil {
		return cid.Undef, err
	}

	rcid, err := t.GetPointer(ctx)
	if err != nil {
		return cid.Undef, err
	}

	ncom := Commit{
		Data: rcid,
	}
	if r.sr.Root.Defined() {
		var com Commit
		if err := r.cst.Get(ctx, r.sr.Root, &com); err != nil {
			return cid.Undef, err
		}
		ncom.Prev = &r.sr.Root
		ncom.Meta = com.Meta
	} else {
		mcid, err := r.cst.Put(ctx, &r.meta)
		if err != nil {
			return cid.Undef, err
		}
		ncom.Meta = mcid
	}

	ncomcid, err := r.cst.Put(ctx, &ncom)
	if err != nil {
		return cid.Undef, err
	}

	nsroot := SignedRoot{
		Root: ncomcid,
	}

	nsrootcid, err := r.cst.Put(ctx, &nsroot)
	if err != nil {
		return cid.Undef, err
	}

	r.sr = nsroot
	r.dirty = false

	return nsrootcid, nil
}

func (r *Repo) getMst(ctx context.Context) (*mst.MerkleSearchTree, error) {
	if r.mst != nil {
		return r.mst, nil
	}

	var com Commit
	if err := r.cst.Get(ctx, r.sr.Root, &com); err != nil {
		return nil, err
	}

	t := mst.LoadMST(r.cst, 32, com.Data)
	r.mst = t
	return t, nil
}

var ErrDoneIterating = fmt.Errorf("done iterating")

func (r *Repo) ForEach(ctx context.Context, prefix string, cb func(k string, v cid.Cid) error) error {
	ctx, span := otel.Tracer("repo").Start(ctx, "ForEach")
	defer span.End()

	var com Commit
	if err := r.cst.Get(ctx, r.sr.Root, &com); err != nil {
		return fmt.Errorf("failed to load commit: %w", err)
	}

	t := mst.LoadMST(r.cst, 32, com.Data)

	if err := t.WalkLeavesFrom(ctx, prefix, func(e mst.NodeEntry) error {
		return cb(e.Key, e.Val)
	}); err != nil {
		if err != ErrDoneIterating {
			return err
		}
	}

	return nil
}

func (r *Repo) GetRecord(ctx context.Context, rpath string) (cid.Cid, cbg.CBORMarshaler, error) {
	ctx, span := otel.Tracer("repo").Start(ctx, "GetRecord")
	defer span.End()

	mst, err := r.getMst(ctx)
	if err != nil {
		return cid.Undef, nil, fmt.Errorf("getting repo mst: %w", err)
	}

	cc, err := mst.Get(ctx, rpath)
	if err != nil {
		return cid.Undef, nil, fmt.Errorf("resolving rpath within mst: %w", err)
	}

	blk, err := r.bs.Get(ctx, cc)
	if err != nil {
		return cid.Undef, nil, err
	}

	rec, err := util.CborDecodeValue(blk.RawData())
	if err != nil {
		fmt.Println("decoding blk: ", cc)
		return cid.Undef, nil, err
	}

	return cc, rec, nil
}

func (r *Repo) DiffSince(ctx context.Context, oldrepo cid.Cid) ([]*mst.DiffOp, error) {
	ctx, span := otel.Tracer("repo").Start(ctx, "DiffSince")
	defer span.End()

	otherRepo, err := OpenRepo(ctx, r.bs, oldrepo)
	if err != nil {
		return nil, err
	}

	oldmst, err := otherRepo.getMst(ctx)
	if err != nil {
		return nil, err
	}

	oldptr, err := oldmst.GetPointer(ctx)
	if err != nil {
		return nil, err
	}

	curmst, err := r.getMst(ctx)
	if err != nil {
		return nil, err
	}

	curptr, err := curmst.GetPointer(ctx)
	if err != nil {
		return nil, err
	}

	return mst.DiffTrees(ctx, r.bs, oldptr, curptr)
}
