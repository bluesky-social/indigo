package repo

import (
	"fmt"
	"sort"

	"github.com/bluesky-social/indigo/atproto/repo/mst"

	"github.com/ipfs/go-cid"
)

// Metadata about update to a single record (key) in the repo.
//
// Used as an abstraction for creating or validating "commit diffs" (eg, `#commit` firehose events)
type Operation struct {
	// key of the record, eg, '{collection}/{record-key}'
	Path string
	// the new record CID value (or nil if this is a deletion)
	Value *cid.Cid
	// the previous record CID value (or nil if this is a creation)
	Prev *cid.Cid
}

func (op *Operation) IsCreate() bool {
	if op.Value != nil && op.Prev == nil {
		return true
	}
	return false
}

func (op *Operation) IsUpdate() bool {
	if op.Value != nil && op.Prev != nil && *op.Value != *op.Prev {
		return true
	}
	return false
}

func (op *Operation) IsDelete() bool {
	if op.Value == nil && op.Prev != nil {
		return true
	}
	return false
}

// Mutates the tree, returning a full `Operation`
func ApplyOp(tree *mst.Tree, path string, val *cid.Cid) (*Operation, error) {
	if val != nil {
		prev, err := tree.Insert([]byte(path), *val)
		if err != nil {
			return nil, err
		}
		op := &Operation{
			Path:  path,
			Value: val,
			Prev:  prev,
		}
		return op, nil
	} else {
		prev, err := tree.Remove([]byte(path))
		if err != nil {
			return nil, err
		}
		op := &Operation{
			Path:  path,
			Value: val,
			Prev:  prev,
		}
		return op, nil
	}
}

// Does a simple "forwards" (not inversion) check of operation
func CheckOp(tree *mst.Tree, op *Operation) error {
	val, err := tree.Get([]byte(op.Path))
	if err != nil {
		return err
	}
	if op.IsCreate() || op.IsUpdate() {
		if val == nil || *val != *op.Value {
			return fmt.Errorf("tree value did not match op: %s %s", op.Path, val)
		}
		return nil
	}
	if op.IsDelete() {
		if val != nil {
			return fmt.Errorf("key still in tree after deletion op: %s", op.Path)
		}
		return nil
	}
	return fmt.Errorf("invalid operation")
}

// Applies the inversion of the `op` to the `tree`. This mutates the tree.
func InvertOp(tree *mst.Tree, op *Operation) error {
	if op.IsCreate() {
		prev, err := tree.Remove([]byte(op.Path))
		if err != nil {
			return fmt.Errorf("failed to invert op: %w", err)
		}
		if prev == nil || *prev != *op.Value {
			return fmt.Errorf("failed to invert creation: previous record CID didn't match")
		}
		return nil
	}
	if op.IsUpdate() {
		prev, err := tree.Insert([]byte(op.Path), *op.Prev)
		if err != nil {
			return fmt.Errorf("failed to invert op: %w", err)
		}
		if prev == nil || *prev != *op.Value {
			return fmt.Errorf("failed to invert update: previous record CID didn't match")
		}
		return nil
	}
	if op.IsDelete() {
		prev, err := tree.Insert([]byte(op.Path), *op.Prev)
		if err != nil {
			return fmt.Errorf("failed to invert op: %w", err)
		}
		if prev != nil {
			return fmt.Errorf("failed to invert deletion: key was previously in tree")
		}
		return nil
	}
	return fmt.Errorf("invalid operation")
}

type opByPath []Operation

func (a opByPath) Len() int      { return len(a) }
func (a opByPath) Swap(i, j int) { a[i], a[j] = a[j], a[i] }

func (a opByPath) Less(i, j int) bool {

	// sort deletions first
	if a[i].IsDelete() && !a[j].IsDelete() {
		return true
	}

	// then by path
	return a[i].Path < a[j].Path
}

// re-orders operation list, and checks for duplicates
func NormalizeOps(list []Operation) ([]Operation, error) {
	// TODO: can this just use the slice ref, instead of returning?

	set := map[string]bool{}
	for _, op := range list {
		if _, ok := set[op.Path]; ok != false {
			return nil, fmt.Errorf("duplicate path in operation list")
		}
		set[op.Path] = true
	}

	sort.Sort(opByPath(list))
	return list, nil
}
