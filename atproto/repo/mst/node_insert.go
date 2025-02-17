package mst

import (
	"fmt"
	"slices"

	"github.com/ipfs/go-cid"
)

// Adds a key/CID entry to a sub-tree defined by a Node. If a previous value existed, returns it.
//
// If the insert is a no-op (the key already existed with exact value), then the operation is a no-op, the tree is not marked dirty, and the val is returned as the 'prev' value.
//
// n: Node at top of sub-tree to operate on
// key: key or path being inserted. must not be empty/nil
// val: CID value being inserted
// height: tree height to insert at, derived from key. if a negative value is provided, will be computed; use -1 instead of 0 if height is not known
func (n *Node) insert(key []byte, val cid.Cid, height int) (*Node, *cid.Cid, error) {
	if n.Stub {
		return nil, nil, ErrPartialTree
	}
	if height < 0 {
		height = HeightForKey(key)
	}

	if n == nil {
		return nil, nil, fmt.Errorf("operating on nil tree/node")
	}

	for height > n.Height {
		// if the new key is higher in the tree; will need to add a parent node, which may involve splitting this current node
		return n.insertParent(key, val, height)
	}

	// if key is lower on the tree, we need to descend first
	if height < n.Height {
		return n.insertChild(key, val, height)
	}

	// look for existing key
	idx := n.findExistingEntry(key)
	if idx >= 0 {
		e := n.Entries[idx]
		if *e.Value == val {
			// same value already exists; no-op
			return n, &val, nil
		}
		// update operation
		prev := e.Value
		n.Entries[idx].Value = &val
		n.Entries[idx].Dirty = true
		n.Dirty = true
		return n, prev, nil
	}

	// insert new entry to this node
	idx, split, err := n.findInsertionIndex(key)
	if err != nil {
		return nil, nil, err
	}
	n.Dirty = true
	newEntry := NodeEntry{
		Key:   key,
		Value: &val,
		Dirty: true,
	}

	if !split {
		// TODO: is this really necessary? or can we just slices.Insert beyond the end of a slice?
		if idx >= len(n.Entries) {
			n.Entries = append(n.Entries, newEntry)
		} else {
			n.Entries = slices.Insert(n.Entries, idx, newEntry)
		}
		return n, nil, nil
	}

	// we need to split
	e := n.Entries[idx]
	left, right, err := e.Child.split(key)
	if err != nil {
		return nil, nil, err
	}
	// remove the existing entry, and replace with three new entries
	n.Entries = slices.Delete(n.Entries, idx, idx+1)
	n.Entries = slices.Insert(
		n.Entries,
		idx,
		NodeEntry{Child: left, Dirty: true},
		newEntry,
		NodeEntry{Child: right, Dirty: true},
	)
	return n, nil, nil
}

func (n *Node) splitEntries(idx int) (*Node, *Node, error) {
	if idx == 0 || idx >= len(n.Entries) {
		return nil, nil, fmt.Errorf("splitting at one end or the other of entries")
	}
	left := Node{
		Height:  n.Height,
		Dirty:   true,
		Entries: n.Entries[:idx],
	}
	right := Node{
		Height: n.Height,
		Dirty:  true,
		// don't use the same slice here
		Entries: append([]NodeEntry{}, n.Entries[idx:]...),
	}
	if left.IsEmpty() || right.IsEmpty() {
		return nil, nil, fmt.Errorf("one of the legs is empty (idx=%d, len=%d)", idx, len(n.Entries))
	}
	return &left, &right, nil
}

func (n *Node) split(key []byte) (*Node, *Node, error) {
	if n.IsEmpty() {
		// TODO: this feels defensive and could be removed
		return nil, nil, fmt.Errorf("tried to split an empty node")
	}

	idx, split, err := n.findInsertionIndex(key)
	if err != nil {
		return nil, nil, err
	}
	if !split {
		// simple split based on values
		return n.splitEntries(idx)
	}

	// need to split recursively
	e := n.Entries[idx]
	lowerLeft, lowerRight, err := e.Child.split(key)
	if err != nil {
		return nil, nil, err
	}
	left := &Node{
		Height:  n.Height,
		Dirty:   true,
		Entries: []NodeEntry{},
	}
	left.Entries = append(left.Entries, n.Entries[:idx]...)
	left.Entries = append(left.Entries, NodeEntry{Child: lowerLeft, Dirty: true})
	right := &Node{
		Height:  n.Height,
		Dirty:   true,
		Entries: []NodeEntry{NodeEntry{Child: lowerRight, Dirty: true}},
	}
	if idx+1 < len(n.Entries) {
		right.Entries = append(right.Entries, n.Entries[idx+1:]...)
	}
	return left, right, nil
}

// inserts a node "above" this node in tree, possibly splitting the current node
func (n *Node) insertParent(key []byte, val cid.Cid, height int) (*Node, *cid.Cid, error) {
	var parent *Node
	if n.IsEmpty() {
		// if current node is empty, just replace directly with current height
		parent = &Node{
			Height: height,
			Dirty:  true,
		}
	} else {
		// otherwise push a layer and recurse
		parent = &Node{
			Height: n.Height + 1,
			Dirty:  true,
			Entries: []NodeEntry{NodeEntry{
				Child: n,
				Dirty: true,
			}},
		}
	}
	// regular insertion will handle any necessary "split"
	return parent.insert(key, val, height)
}

// inserts a node "below" this node in tree; either creating a new child entry or re-using an existing one
func (n *Node) insertChild(key []byte, val cid.Cid, height int) (*Node, *cid.Cid, error) {
	// look for an existing child node which encompasses the key, and use that
	idx := n.findExistingChild(key)
	if idx >= 0 {
		e := n.Entries[idx]
		if e.Child == nil {
			return nil, nil, fmt.Errorf("could not insert key: %w", ErrPartialTree)
		}
		newChild, prev, err := e.Child.insert(key, val, height)
		if err != nil {
			return nil, nil, err
		}
		if prev != nil && *prev == val {
			// no-op
			return n, &val, nil
		}
		n.Dirty = true
		n.Entries[idx].Child = newChild
		n.Entries[idx].Dirty = true
		return n, prev, nil
	}

	// insert a new child node. this might be recursive if the child is not a *direct* child
	idx, split, err := n.findInsertionIndex(key)
	if err != nil {
		return nil, nil, err
	}
	if split {
		return nil, nil, fmt.Errorf("unexpected split when inserting child")
	}
	n.Dirty = true
	newChild := &Node{
		Height: n.Height - 1,
		Dirty:  true,
	}
	newChild, _, err = newChild.insert(key, val, height)
	if err != nil {
		return nil, nil, err
	}
	newEntry := NodeEntry{
		Child: newChild,
		Dirty: true,
	}
	if idx == len(n.Entries) {
		n.Entries = append(n.Entries, newEntry)
	} else {
		n.Entries = slices.Insert(n.Entries, idx, newEntry)
	}
	return n, nil, nil
}
