package mst

import (
	"context"
	"fmt"

	"github.com/bluesky-social/indigo/util"
	cid "github.com/ipfs/go-cid"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	cbor "github.com/ipfs/go-ipld-cbor"
)

type DiffOp struct {
	Depth  int
	Op     string
	Rpath  string
	OldCid cid.Cid
	NewCid cid.Cid
}

// TODO: this code isn't great, should be rewritten on top of the baseline datastructures once functional and correct
func DiffTrees(ctx context.Context, bs blockstore.Blockstore, from, to cid.Cid) ([]*DiffOp, error) {
	return DiffTreesVisitor(ctx, bs, from, to, func(cid.Cid) {})
}

func DiffTreesVisitor(ctx context.Context, bs blockstore.Blockstore, from, to cid.Cid, visit func(cid.Cid)) ([]*DiffOp, error) {
	var cst cbor.IpldStore = util.CborStore(bs)

	if from == cid.Undef {
		cst = &util.CallbackWrapCborStore{
			Cst:    cst,
			ReadCb: visit,
		}
		return identityDiff(ctx, cst, to)
	}

	if from != to {
		visit(to)
	}

	ft := LoadMST(cst, from)
	tt := LoadMST(cst, to)

	fents, err := ft.getEntries(ctx)
	if err != nil {
		return nil, fmt.Errorf("get 'from' entries: %w", err)
	}

	tents, err := tt.getEntries(ctx)
	if err != nil {
		return nil, fmt.Errorf("get 'to' entries: %w", err)
	}

	var ixf, ixt int
	var out []*DiffOp
	for ixf < len(fents) && ixt < len(tents) {
		ef := fents[ixf]
		et := tents[ixt]

		if nodeEntriesEqual(&ef, &et) {
			ixf++
			ixt++
			continue
		}

		if ef.isLeaf() && et.isLeaf() {
			if ef.Key == et.Key {
				if ef.Val == et.Val {
					return nil, fmt.Errorf("hang on, why are these leaves equal?")
				}

				visit(et.Val)

				out = append(out, &DiffOp{
					Op:     "mut",
					Rpath:  ef.Key,
					OldCid: ef.Val,
					NewCid: et.Val,
				})
				ixf++
				ixt++
				continue
			}
			// different keys... what do?

			// if the 'to' key is earlier than the 'from' key, call it an insert
			// otherwise call it a deletion?

			if ef.Key > et.Key {
				visit(et.Val)
				out = append(out, &DiffOp{
					Op:     "add",
					Rpath:  et.Key,
					NewCid: et.Val,
				})
				// only walk forward the pointer that was 'behind'
				ixt++
			} else {
				out = append(out, &DiffOp{
					Op:     "del",
					Rpath:  ef.Key,
					OldCid: ef.Val,
				})
				// only walk forward the pointer that was 'behind'
				ixf++
			}

			// call it an insertion?
			continue
		}

		if ef.isTree() {
			sub, err := ef.Tree.getEntries(ctx)
			if err != nil {
				return nil, err
			}

			fents = append(sub, fents[ixf+1:]...)
			ixf = 0
			continue
		}

		if et.isTree() {
			visit(et.Tree.pointer)
			sub, err := et.Tree.getEntries(ctx)
			if err != nil {
				return nil, err
			}

			tents = append(sub, tents[ixt+1:]...)
			ixt = 0
			continue
		}
	}

	for ; ixf < len(fents); ixf++ {
		// deletions

		e := fents[ixf]
		if e.isLeaf() {
			out = append(out, &DiffOp{
				Op:     "del",
				Rpath:  e.Key,
				OldCid: e.Val,
			})

		} else if e.isTree() {
			if err := e.Tree.WalkLeavesFrom(ctx, "", func(n NodeEntry) error {
				out = append(out, &DiffOp{
					Op:     "del",
					Rpath:  n.Key,
					OldCid: n.Val,
				})
				return nil
			}); err != nil {
				return nil, err
			}
		}
	}

	for ; ixt < len(tents); ixt++ {
		// insertions

		e := tents[ixt]
		if e.isLeaf() {
			visit(e.Val)
			out = append(out, &DiffOp{
				Op:     "add",
				Rpath:  e.Key,
				NewCid: e.Val,
			})

		} else if e.isTree() {
			visit(e.Tree.pointer)
			e.Tree.cst = &util.CallbackWrapCborStore{
				Cst:    cst,
				ReadCb: visit,
			}
			if err := e.Tree.WalkLeavesFrom(ctx, "", func(n NodeEntry) error {
				visit(n.Val)
				out = append(out, &DiffOp{
					Op:     "add",
					Rpath:  n.Key,
					NewCid: n.Val,
				})
				return nil
			}); err != nil {
				return nil, err
			}
		}
	}

	return out, nil
}

func nodeEntriesEqual(a, b *NodeEntry) bool {
	if !(a.Key == b.Key && a.Val == b.Val) {
		return false
	}

	if a.Tree == nil && b.Tree == nil {
		return true
	}

	if a.Tree != nil && b.Tree != nil && a.Tree.pointer == b.Tree.pointer {
		return true
	}

	return false
}

func identityDiff(ctx context.Context, cst cbor.IpldStore, root cid.Cid) ([]*DiffOp, error) {
	fmt.Println("IDENTITY DIFF")
	tt := LoadMST(cst, root)

	var ops []*DiffOp
	if err := tt.WalkLeavesFrom(ctx, "", func(ne NodeEntry) error {
		ops = append(ops, &DiffOp{
			Op:     "add",
			Rpath:  ne.Key,
			NewCid: ne.Val,
		})
		return nil
	}); err != nil {
		return nil, err
	}
	return ops, nil
}
