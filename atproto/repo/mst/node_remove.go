package mst

import (
	"bytes"
	"fmt"
	"slices"

	"github.com/ipfs/go-cid"
)

// Removes key/value from the sub-tree provided, returning a new tree, and the previous CID value. If key is not found, returns unmodified subtree, and nil for the returned CID.
//
// n: Node at top of sub-tree to operate on. Must not be nil.
// key: key or path being inserted. must not be empty/nil
// height: tree height corresponding to key. if a negative value is provided, will be computed; use -1 instead of 0 if height is not known
func (n *Node) remove(key []byte, height int) (*Node, *cid.Cid, error) {
	if n.Stub {
		return nil, nil, ErrPartialTree
	}
	// TODO: do we need better handling of "is this the top"?
	top := false
	if height < 0 {
		top = true
		height = HeightForKey(key)
	}

	if height > n.Height {
		// removing a key from a higher layer; key was not in tree
		return n, nil, nil
	}

	if height < n.Height {
		// TODO: handle case of this returning an empty node at top of tree, with wrong height
		return n.removeChild(key, height)
	}

	// look at this level
	idx := n.findExistingEntry(key)
	if idx < 0 {
		// key not found
		return n, nil, nil
	}

	// found it! will remove from list
	n.Dirty = true
	prev := n.Entries[idx].Value

	// check if we need to "merge" adjacent nodes
	if idx > 0 && idx+1 < len(n.Entries) && n.Entries[idx-1].IsChild() && n.Entries[idx+1].IsChild() {
		if n.Entries[idx-1].Child == nil || n.Entries[idx+1].Child == nil {
			return nil, nil, fmt.Errorf("can not merge child nodes: %w", ErrPartialTree)
		}
		newChild, err := mergeNodes(n.Entries[idx-1].Child, n.Entries[idx+1].Child)
		if err != nil {
			return nil, nil, err
		}
		n.Entries = slices.Delete(n.Entries, idx, idx+2)
		n.Entries[idx-1] = NodeEntry{Child: newChild, Dirty: true}
	} else {
		// simple removal
		n.Entries = slices.Delete(n.Entries, idx, idx+1)
	}

	// marks adjacent child nodes dirty to include as "proof"
	proveDeletion(n, key)

	// check if top of node is now just a pointer
	if top {
		for {
			if len(n.Entries) != 1 || !n.Entries[0].IsChild() {
				break
			}
			if n.Entries[0].Child == nil {
				// this is something of a hack, for MST inversion which requires trimming the tree
				if n.Entries[0].ChildCID == nil {
					return nil, nil, fmt.Errorf("can not prune top of tree: %w", ErrPartialTree)
				} else {
					n = &Node{
						Height: n.Height - 1,
						Stub:   true,
						CID:    n.Entries[0].ChildCID,
					}
				}
			} else {
				n = n.Entries[0].Child
			}
		}
	}
	return n, prev, nil
}

func proveDeletion(n *Node, key []byte) error {
	for i, e := range n.Entries {
		if e.IsValue() {
			if bytes.Compare(key, e.Key) < 0 {
				return nil
			}
		}
		if e.IsChild() {
			// first, see if there is a next entry as a value which this key would be after; if so we can skip checking this child
			if i+1 < len(n.Entries) {
				next := n.Entries[i+1]
				if next.IsValue() && bytes.Compare(key, next.Key) > 0 {
					continue
				}
			}
			if e.Child == nil {
				return fmt.Errorf("can't prove deletion: %w", ErrPartialTree)
			}
			order, err := e.Child.compareKey(key, true)
			if err != nil {
				return err
			}
			if order > 0 {
				// key comes after this entire child sub-tree
				continue
			}
			if order < 0 {
				return nil
			}
			// key falls inside this child sub-tree
			return proveDeletion(e.Child, key)
		}
	}
	return nil
}

func mergeNodes(left *Node, right *Node) (*Node, error) {
	idx := len(left.Entries)
	n := &Node{
		Height:  left.Height,
		Dirty:   true,
		Entries: append(left.Entries, right.Entries...),
	}
	if n.Entries[idx-1].IsChild() && n.Entries[idx].IsChild() {
		// need to merge recursively
		lowerLeft := n.Entries[idx-1]
		lowerRight := n.Entries[idx]
		if lowerLeft.Child == nil || lowerRight.Child == nil {
			return nil, fmt.Errorf("can not merge child nodes: %w", ErrPartialTree)
		}
		lowerMerged, err := mergeNodes(lowerLeft.Child, lowerRight.Child)
		if err != nil {
			return nil, err
		}
		n.Entries[idx-1] = NodeEntry{Child: lowerMerged, Dirty: true}
		n.Entries = slices.Delete(n.Entries, idx, idx+1)
	}
	return n, nil
}

// internal helper
func (n *Node) removeChild(key []byte, height int) (*Node, *cid.Cid, error) {
	// look for a child
	idx := n.findExistingChild(key)
	if idx < 0 {
		// no child pointer; key not in tree
		return n, nil, nil
	}

	e := n.Entries[idx]
	if e.Child == nil {
		// partial node, can't recurse
		return nil, nil, fmt.Errorf("could not remove key: %w", ErrPartialTree)
	}
	newChild, prev, err := e.Child.remove(key, height)
	if err != nil {
		return nil, nil, err
	}
	if prev == nil {
		// no-op
		return n, nil, nil
	}

	// if the child node was updated, but still exists, just update pointer
	if !newChild.IsEmpty() {
		n.Dirty = true
		n.Entries[idx].Child = newChild
		n.Entries[idx].Dirty = true
		return n, prev, nil
	}

	// if new child was empty, remove it from entry list; note that *this* entry might now be empty
	n.Dirty = true
	n.Entries = slices.Delete(n.Entries, idx, idx+1)
	return n, prev, nil
}
