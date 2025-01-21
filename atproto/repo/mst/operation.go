package mst

import (
	"fmt"

	"github.com/ipfs/go-cid"
)

type Operation struct {
	Path  string
	Value *cid.Cid
	Prev  *cid.Cid
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
func ApplyOp(n *Node, path string, val *cid.Cid) (*Node, *Operation, error) {
	if val != nil {
		n, prev, err := Insert(n, []byte(path), *val, -1)
		if err != nil {
			return nil, nil, err
		}
		op := &Operation{
			Path:  path,
			Value: val,
			Prev:  prev,
		}
		return n, op, nil
	} else {
		n, prev, err := Remove(n, []byte(path), -1)
		if err != nil {
			return nil, nil, err
		}
		op := &Operation{
			Path:  path,
			Value: val,
			Prev:  prev,
		}
		return n, op, nil
	}
}

// Does a simple "forwards" (not inversion) check of operation
func CheckOp(n *Node, op *Operation) error {
	val, err := Get(n, []byte(op.Path), -1)
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

func InvertOp(n *Node, op *Operation) (*Node, error) {
	if op.IsCreate() {
		n, prev, err := Remove(n, []byte(op.Path), -1)
		if err != nil {
			return nil, fmt.Errorf("failed to invert op: %w", err)
		}
		if prev == nil || *prev != *op.Value {
			return nil, fmt.Errorf("failed to invert creation")
		}
		return n, nil
	}
	if op.IsUpdate() {
		n, prev, err := Insert(n, []byte(op.Path), *op.Prev, -1)
		if err != nil {
			return nil, fmt.Errorf("failed to invert op: %w", err)
		}
		if prev == nil || *prev != *op.Value {
			return nil, fmt.Errorf("failed to invert update")
		}
		return n, nil
	}
	if op.IsDelete() {
		n, prev, err := Insert(n, []byte(op.Path), *op.Prev, -1)
		if err != nil {
			return nil, fmt.Errorf("failed to invert op: %w", err)
		}
		if prev != nil {
			return nil, fmt.Errorf("failed to invert deletion")
		}
		return n, nil
	}
	return nil, fmt.Errorf("invalid operation")
}
