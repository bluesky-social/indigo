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
	"github.com/whyrusleeping/gosky/mst"
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

	meta Meta

	mst *mst.MerkleSearchTree

	dirty bool
}

func IngestRepo(ctx context.Context, bs blockstore.Blockstore, r io.Reader) (cid.Cid, error) {
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
		cst: cst,
	}, nil
}

type CborMarshaler interface {
	MarshalCBOR(w io.Writer) error
}

func (r *Repo) CreateRecord(ctx context.Context, nsid string, rec CborMarshaler) error {
	r.dirty = true
	t, err := r.getMst(ctx)
	if err != nil {
		return fmt.Errorf("failed to get mst: %w", err)
	}

	k, err := r.cst.Put(ctx, rec)
	if err != nil {
		return err
	}

	nmst, err := t.Add(ctx, nsid+"/"+NextTID(), k, -1)
	if err != nil {
		return fmt.Errorf("mst.Add failed: %w", err)
	}

	r.mst = nmst
	return nil
}

func (r *Repo) Commit(ctx context.Context) (cid.Cid, error) {
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
	fmt.Println("rootcid output: ", nsrootcid)

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

func (r *Repo) ForEach(ctx context.Context, prefix string, cb func(k string, v cid.Cid) error) error {
	var com Commit
	if err := r.cst.Get(ctx, r.sr.Root, &com); err != nil {
		return err
	}

	t := mst.LoadMST(r.cst, 32, com.Data)

	if err := t.WalkLeavesFrom(ctx, prefix, func(e mst.NodeEntry) error {
		return cb(e.Key, e.Val)
	}); err != nil {
		return err
	}

	return nil
}
