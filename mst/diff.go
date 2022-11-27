package mst

import (
	"context"
	"fmt"
	"sort"

	cid "github.com/ipfs/go-cid"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	cbor "github.com/ipfs/go-ipld-cbor"
)

type DiffOp struct {
	Depth  int
	Op     string
	Tid    string
	OldCid cid.Cid
	NewCid cid.Cid
}

func DiffTrees(ctx context.Context, bs blockstore.Blockstore, from, to cid.Cid) ([]*DiffOp, error) {
	cst := cbor.NewCborStore(bs)

	ft := LoadMST(cst, 32, from)
	tt := LoadMST(cst, 32, to)

	return diffTreesRec(ctx, cst, ft, tt, 1)
}

func checkDiffSort(diffs []*DiffOp) {
	if !sort.SliceIsSorted(diffs, func(i, j int) bool {
		return diffs[i].Tid < diffs[j].Tid
	}) {
		panic(fmt.Sprintf("diff results not properly sorted! %d", len(diffs)))
	}
}

func diffTreesRec(ctx context.Context, cst cbor.IpldStore, ft, tt *MerkleSearchTree, depth int) ([]*DiffOp, error) {
	// TODO: this code isnt great, should be rewritten on top of the baseline datastructures once functional and correct
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
					Tid:    ef.Key,
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
					Tid:    et.Key,
					NewCid: et.Val,
				})
				// only walk forward the pointer that was 'behind'
				ixt++
			} else {
				out = append(out, &DiffOp{
					Op:     "del",
					Tid:    ef.Key,
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
				Tid:    e.Key,
				OldCid: e.Val,
			})

		} else if e.isTree() {
			if err := e.Tree.WalkLeavesFrom(ctx, "", func(n NodeEntry) error {
				out = append(out, &DiffOp{
					Op:     "del",
					Tid:    n.Key,
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
			out = append(out, &DiffOp{
				Op:     "add",
				Tid:    e.Key,
				NewCid: e.Val,
			})

		} else if e.isTree() {
			if err := e.Tree.WalkLeavesFrom(ctx, "", func(n NodeEntry) error {
				out = append(out, &DiffOp{
					Op:     "add",
					Tid:    n.Key,
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

func sameCidPtr(a, b *cid.Cid) bool {
	if a == nil && b == nil {
		return true
	}
	if a != nil && b != nil && *a == *b {
		return true
	}
	return false
}
