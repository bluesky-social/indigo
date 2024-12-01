package mst

import (
	"context"
	"fmt"

	"github.com/bluesky-social/indigo/util"
	"github.com/ipfs/boxo/blockstore"
	cid "github.com/ipfs/go-cid"
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
	cst := util.CborStore(bs)

	if from == cid.Undef {
		return identityDiff(ctx, bs, to)
	}

	ft := LoadMST(cst, from)
	tt := LoadMST(cst, to)

	fents, err := ft.getEntries(ctx)
	if err != nil {
		return nil, err
	}

	tents, err := tt.getEntries(ctx)
	if err != nil {
		return nil, err
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
			if err := e.Tree.WalkLeavesFrom(ctx, "", func(key string, val cid.Cid) error {
				out = append(out, &DiffOp{
					Op:     "del",
					Rpath:  key,
					OldCid: val,
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
			out = append(out, &DiffOp{
				Op:     "add",
				Rpath:  e.Key,
				NewCid: e.Val,
			})

		} else if e.isTree() {
			if err := e.Tree.WalkLeavesFrom(ctx, "", func(key string, val cid.Cid) error {
				out = append(out, &DiffOp{
					Op:     "add",
					Rpath:  key,
					NewCid: val,
				})
				return nil
			}); err != nil {
				return nil, err
			}
		}
	}

	return out, nil
}

func nodeEntriesEqual(a, b *nodeEntry) bool {
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

func identityDiff(ctx context.Context, bs blockstore.Blockstore, root cid.Cid) ([]*DiffOp, error) {
	cst := util.CborStore(bs)
	tt := LoadMST(cst, root)

	var ops []*DiffOp
	if err := tt.WalkLeavesFrom(ctx, "", func(key string, val cid.Cid) error {
		ops = append(ops, &DiffOp{
			Op:     "add",
			Rpath:  key,
			NewCid: val,
		})
		return nil
	}); err != nil {
		return nil, err
	}
	return ops, nil
}
