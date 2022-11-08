package repo

import (
	"context"
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
	AuthToken *string `cborgen:"auth_token"`
	Data      cid.Cid `cborgen:"data"`
	Meta      cid.Cid `cborgen:"meta"`
	Prev      cid.Cid `cborgen:"prev"`
}

type Meta struct {
	Datastore string `cborgen:"datastore"`
	Did       string `cborgen:"did"`
	Version   int64  `cborgen:"version"`
}

type Repo struct {
	sr  SignedRoot
	cst cbor.IpldStore
}

func ReadRepoFromCar(ctx context.Context, r io.Reader) (*Repo, error) {
	br, err := car.NewBlockReader(r)
	if err != nil {
		return nil, err
	}

	bs := blockstore.NewBlockstore(datastore.NewMapDatastore())

	for {
		blk, err := br.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		if err := bs.Put(ctx, blk); err != nil {
			return nil, err
		}
	}

	cst := cbor.NewCborStore(bs)

	var sr SignedRoot
	if err := cst.Get(ctx, br.Roots[0], &sr); err != nil {
		return nil, err
	}

	return &Repo{
		sr:  sr,
		cst: cst,
	}, nil
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
