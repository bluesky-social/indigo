package repo

import (
	"bytes"
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

// current version of repo currently implemented
const ATP_REPO_VERSION int64 = 2

type SignedCommit struct {
	Did     string   `cborgen:"did"`
	Version int64    `cborgen:"version"`
	Prev    *cid.Cid `cborgen:"prev"`
	Data    cid.Cid  `cborgen:"data"`
	Sig     []byte   `cborgen:"sig"`
}

type UnsignedCommit struct {
	Did     string   `cborgen:"did"`
	Version int64    `cborgen:"version"`
	Prev    *cid.Cid `cborgen:"prev"`
	Data    cid.Cid  `cborgen:"data"`
}

type Repo struct {
	sc  SignedCommit
	cst cbor.IpldStore
	bs  blockstore.Blockstore

	repoCid cid.Cid

	mst *mst.MerkleSearchTree

	dirty bool
}

// Returns a copy of commit without the Sig field. Helpful when verifying signature.
func (sc *SignedCommit) Unsigned() *UnsignedCommit {
	return &UnsignedCommit{
		Did:     sc.Did,
		Version: sc.Version,
		Prev:    sc.Prev,
		Data:    sc.Data,
	}
}

// returns bytes of the DAG-CBOR representation of object. This is what gets
// signed; the `go-did` library will take the SHA-256 of the bytes and sign
// that.
func (uc *UnsignedCommit) BytesForSigning() ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := uc.MarshalCBOR(buf); err != nil {
		return []byte{}, err
	}
	return buf.Bytes(), nil
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

	return OpenRepo(ctx, bs, root, false)
}

func NewRepo(ctx context.Context, did string, bs blockstore.Blockstore) *Repo {
	cst := util.CborStore(bs)

	t := mst.NewMST(cst, cid.Undef, []mst.NodeEntry{}, 0)
	sc := SignedCommit{
		Did:     did,
		Version: 2,
	}

	return &Repo{
		cst:   cst,
		bs:    bs,
		mst:   t,
		sc:    sc,
		dirty: true,
	}
}

func OpenRepo(ctx context.Context, bs blockstore.Blockstore, root cid.Cid, fullRepo bool) (*Repo, error) {
	cst := util.CborStore(bs)

	var sc SignedCommit
	if err := cst.Get(ctx, root, &sc); err != nil {
		return nil, fmt.Errorf("loading root from blockstore: %w", err)
	}

	if sc.Version != ATP_REPO_VERSION {
		return nil, fmt.Errorf("unsupported repo version: %d", sc.Version)
	}

	return &Repo{
		sc:      sc,
		bs:      bs,
		cst:     cst,
		repoCid: root,
	}, nil
}

type CborMarshaler interface {
	MarshalCBOR(w io.Writer) error
}

func (r *Repo) RepoDid() string {
	if r.sc.Did == "" {
		panic("repo has unset did")
	}

	return r.sc.Did
}

// TODO(bnewbold): this could return just *cid.Cid
func (r *Repo) PrevCommit(ctx context.Context) (*cid.Cid, error) {
	return r.sc.Prev, nil
}

func (r *Repo) SignedCommit() SignedCommit {
	return r.sc
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

// creates and writes a new SignedCommit for this repo, with `prev` pointing to old value
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

	var nprev *cid.Cid
	if r.repoCid.Defined() {
		nprev = &r.repoCid
	}

	ncom := UnsignedCommit{
		Did:     r.RepoDid(),
		Version: ATP_REPO_VERSION,
		Prev:    nprev,
		Data:    rcid,
	}

	sb, err := ncom.BytesForSigning()
	if err != nil {
		return cid.Undef, fmt.Errorf("failed to serialize commit: %w", err)
	}
	sig, err := signer(ctx, ncom.Did, sb)
	if err != nil {
		return cid.Undef, fmt.Errorf("failed to sign root: %w", err)
	}

	nsc := SignedCommit{
		Sig:     sig,
		Did:     ncom.Did,
		Version: ncom.Version,
		Prev:    ncom.Prev,
		Data:    ncom.Data,
	}

	nsccid, err := r.cst.Put(ctx, &nsc)
	if err != nil {
		return cid.Undef, err
	}

	r.sc = nsc
	r.dirty = false

	return nsccid, nil
}

func (r *Repo) getMst(ctx context.Context) (*mst.MerkleSearchTree, error) {
	if r.mst != nil {
		return r.mst, nil
	}

	t := mst.LoadMST(r.cst, r.sc.Data)
	r.mst = t
	return t, nil
}

var ErrDoneIterating = fmt.Errorf("done iterating")

func (r *Repo) ForEach(ctx context.Context, prefix string, cb func(k string, v cid.Cid) error) error {
	ctx, span := otel.Tracer("repo").Start(ctx, "ForEach")
	defer span.End()

	t := mst.LoadMST(r.cst, r.sc.Data)

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
		otherRepo, err := OpenRepo(ctx, r.bs, oldrepo, true)
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
