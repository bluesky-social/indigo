package repo

import (
	"context"
	"fmt"
	"io"

	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/mst"
	"github.com/bluesky-social/indigo/util"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/ipld/go-car/v2"
	cbg "github.com/whyrusleeping/cbor-gen"
	"go.opentelemetry.io/otel"
)

type SignedCommit struct {
	Root cid.Cid `cborgen:"root"`
	Sig  []byte  `cborgen:"sig"`
}

type Root struct {
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
	sr  SignedCommit
	cst cbor.IpldStore
	bs  blockstore.Blockstore

	repoCid cid.Cid

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

func NewRepo(ctx context.Context, did string, bs blockstore.Blockstore) *Repo {
	cst := util.CborStore(bs)

	t := mst.NewMST(cst, cid.Undef, []mst.NodeEntry{}, 0)

	meta := Meta{
		Datastore: "TODO",
		Did:       did,
		Version:   1,
	}

	return &Repo{
		meta:  meta,
		cst:   cst,
		bs:    bs,
		mst:   t,
		dirty: true,
	}
}

func OpenRepo(ctx context.Context, bs blockstore.Blockstore, root cid.Cid) (*Repo, error) {
	cst := util.CborStore(bs)

	var sr SignedCommit
	if err := cst.Get(ctx, root, &sr); err != nil {
		return nil, fmt.Errorf("loading root from blockstore: %w", err)
	}

	var rt Root
	if err := cst.Get(ctx, sr.Root, &rt); err != nil {
		return nil, fmt.Errorf("loading root: %w", err)
	}

	var meta Meta
	if err := cst.Get(ctx, rt.Meta, &meta); err != nil {
		return nil, fmt.Errorf("loading meta: %w", err)
	}

	return &Repo{
		sr:      sr,
		bs:      bs,
		cst:     cst,
		repoCid: root,
		meta:    meta,
	}, nil
}

type CborMarshaler interface {
	MarshalCBOR(w io.Writer) error
}

func (r *Repo) MetaCid(ctx context.Context) (cid.Cid, error) {
	var root Root
	if err := r.cst.Get(ctx, r.sr.Root, &root); err != nil {
		return cid.Undef, err
	}

	return root.Meta, nil
}

func (r *Repo) RepoDid() string {
	if r.meta.Did == "" {
		panic("repo has unset did")
	}

	return r.meta.Did
}

func (r *Repo) PrevCommit(ctx context.Context) (*cid.Cid, error) {
	var c Root
	if err := r.cst.Get(ctx, r.sr.Root, &c); err != nil {
		return nil, fmt.Errorf("loading previous commit: %w", err)
	}

	return c.Prev, nil
}

func (r *Repo) CommitRoot() cid.Cid {
	return r.sr.Root
}

func (r *Repo) SignedCommit() SignedCommit {
	return r.sr
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

func (r *Repo) Commit(ctx context.Context, signer func(context.Context, string, []byte) ([]byte, error)) (cid.Cid, error) {
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

	nroot := Root{
		Data: rcid,
	}

	if r.repoCid.Defined() {
		var rt Root
		if err := r.cst.Get(ctx, r.sr.Root, &rt); err != nil {
			return cid.Undef, err
		}
		nroot.Prev = &r.repoCid
		nroot.Meta = rt.Meta
	} else {
		mcid, err := r.cst.Put(ctx, &r.meta)
		if err != nil {
			return cid.Undef, err
		}
		nroot.Meta = mcid
	}

	ncomcid, err := r.cst.Put(ctx, &nroot)
	if err != nil {
		return cid.Undef, err
	}

	did := r.RepoDid()

	sig, err := signer(ctx, did, ncomcid.Bytes())
	if err != nil {
		return cid.Undef, fmt.Errorf("failed to sign root: %w", err)
	}

	nsroot := SignedCommit{
		Root: ncomcid,
		Sig:  sig,
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

	var rt Root
	if err := r.cst.Get(ctx, r.sr.Root, &rt); err != nil {
		return nil, err
	}

	t := mst.LoadMST(r.cst, rt.Data)
	r.mst = t
	return t, nil
}

var ErrDoneIterating = fmt.Errorf("done iterating")

func (r *Repo) ForEach(ctx context.Context, prefix string, cb func(k string, v cid.Cid) error) error {
	ctx, span := otel.Tracer("repo").Start(ctx, "ForEach")
	defer span.End()

	var rt Root
	if err := r.cst.Get(ctx, r.sr.Root, &rt); err != nil {
		return fmt.Errorf("failed to load commit: %w", err)
	}

	t := mst.LoadMST(r.cst, rt.Data)

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

	rec, err := lexutil.CborDecodeValue(blk.RawData())
	if err != nil {
		return cid.Undef, nil, err
	}

	return cc, rec, nil
}

func (r *Repo) DiffSince(ctx context.Context, oldrepo cid.Cid) ([]*mst.DiffOp, error) {
	ctx, span := otel.Tracer("repo").Start(ctx, "DiffSince")
	defer span.End()

	var oldTree cid.Cid
	if oldrepo.Defined() {
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
		oldTree = oldptr
	}

	curmst, err := r.getMst(ctx)
	if err != nil {
		return nil, err
	}

	curptr, err := curmst.GetPointer(ctx)
	if err != nil {
		return nil, err
	}

	return mst.DiffTrees(ctx, r.bs, oldTree, curptr)
}
