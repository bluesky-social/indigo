package mst

import (
	"bytes"
	"fmt"

	"github.com/ipfs/go-cid"
)

// Represents a node in a Merkle Search Tree (MST). If this is the "root" or "top" of the tree, it effectively is the tree itself.
//
// Trees may be "partial" if they contain references to child nodes by CID, but not pointers to `Node` representations.
type Node struct {
	// array of key/value pairs and pointers to child nodes. entry arrays must always be in correct/valid order at any point in time: sorted by 'key', and at most one 'pointer' entry between 'value' entries.
	Entries []NodeEntry
	// "height" or "layer" of MST tree this node is at (with zero at the "bottom" and root/top of tree the "highest")
	Height int
	// if true, the cached CID of this node is out of date
	Dirty bool
	// optionally, the last computed CID of this Node (when expressed as NodeData)
	CID *cid.Cid
	// if true, this is an empty/incomplete node which just represents the CID of the tree. only used as part of MST inversion
	Stub bool
}

// Represents an entry in an MST `Node`, which could either be a direct path/value entry, or a pointer do a child tree node. Note that these are *not* one-to-one with `EntryData`.
//
// Either the Key and Value fields should be non-zero; or the Child and/or ChildCID field should be non-zero.
// If ChildCID is present, but Child is not, then this is part of a "partial" tree.
type NodeEntry struct {
	Key      []byte
	Value    *cid.Cid
	ChildCID *cid.Cid
	Child    *Node

	// tracks whether anything about this entry has changed since `Node` CID was computed
	Dirty bool
}

func (n *Node) IsEmpty() bool {
	return len(n.Entries) == 0
}

// Checks if the sub-tree (this node, or any children, recursively) contains any CID references to nodes which are not present.
func (n *Node) IsPartial() bool {
	if n.Stub {
		return true
	}
	for _, e := range n.Entries {
		if e.ChildCID != nil && e.Child == nil {
			return true
		}
		if e.Child != nil && e.Child.IsPartial() {
			return true
		}
	}
	return false
}

// Returns true if this entry is a key/value at the current node
func (e *NodeEntry) IsValue() bool {
	if len(e.Key) > 0 && e.Value != nil {
		return true
	}
	return false
}

// Returns true if this entry points to a node on a lower level
func (e *NodeEntry) IsChild() bool {
	if e.Child != nil || e.ChildCID != nil {
		return true
	}
	return false
}

// creates a deep/recursive copy of the sub-tree
func (n *Node) deepCopy() *Node {
	out := Node{
		Entries: make([]NodeEntry, len(n.Entries)),
		Height:  n.Height,
		Dirty:   n.Dirty,
		Stub:    n.Stub,
		CID:     n.CID,
	}
	for i, e := range n.Entries {
		out.Entries[i] = NodeEntry{
			Key:      e.Key,
			Value:    e.Value,
			ChildCID: e.ChildCID,
			Dirty:    e.Dirty,
		}
		if e.Child != nil {
			out.Entries[i].Child = e.Child.deepCopy()
		}
	}
	return &out
}

// Looks for a "value" entry in the node with the exact key.
// Returns entry index if a matching entry is found; or -1 if not found
func (n *Node) findExistingEntry(key []byte) int {
	for i, e := range n.Entries {
		// TODO perf: could skip early if e.Key is lower
		if e.IsValue() && bytes.Equal(key, e.Key) {
			return i
		}
	}
	return -1
}

// Looks for a "child" entry which the key would live under.
//
// Returns -1 if not found.
func (n *Node) findExistingChild(key []byte) int {
	idx := -1
	for i, e := range n.Entries {
		if e.IsChild() {
			idx = i
			continue
		}
		if e.IsValue() {
			if bytes.Compare(key, e.Key) <= 0 {
				break
			}
			idx = -1
		}
	}
	return idx
}

// Determines index where a new entry (child or value) would be inserted, relevant to the given key.
//
// If the key would "split" an existing child entry, the index of that entry is returned, and a flag set
//
// If the entry would be appended, then the index returned will be one higher that the current largest index.
func (n *Node) findInsertionIndex(key []byte) (idx int, split bool, retErr error) {
	if n.Stub {
		return -1, false, fmt.Errorf("partial MST, can't determine insertion order")
	}
	for i, e := range n.Entries {
		if e.IsValue() {
			if bytes.Compare(key, e.Key) < 0 {
				return i, false, nil
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
				return -1, false, fmt.Errorf("partial MST, can't determine insertion order")
			}
			order, err := e.Child.compareKey(key, false)
			if err != nil {
				return -1, false, err
			}
			if order < 0 {
				// key comes before this entire child sub-tree
				return i, false, nil
			}
			if order > 0 {
				// key comes after this entire child sub-tree
				continue
			}
			// key falls inside this child sub-tree
			return i, true, nil
		}
	}

	// would need to be appended after
	return len(n.Entries), false, nil
}

// Compares a provided `key` against the overall range of keys represented by a `Node`. Returns -1 if the key sorts lower than all keys (recursively) covered by the Node; 1 if higher, and 0 if the key falls within Node's key range.
//
// If the `markDirty` flag is true, then this method will set the Dirty flag on this node, and any child nodes which were needed to "prove" the key order. This can be used to mark nodes for inclusion in invertible MST diffs.
func (n *Node) compareKey(key []byte, markDirty bool) (int, error) {
	if n.Stub {
		return -1, ErrPartialTree
	}
	if n.IsEmpty() {
		// TODO: should we actually return 0 in this case?
		return 0, fmt.Errorf("can't determine key range of empty MST node")
	}
	if markDirty == true {
		n.Dirty = true
	}
	// check if lower than this entire node
	e := n.Entries[0]
	if e.IsValue() && bytes.Compare(key, e.Key) < 0 {
		return -1, nil
	}
	// check if higher than this entire node
	e = n.Entries[len(n.Entries)-1]
	if e.IsValue() && bytes.Compare(key, e.Key) > 0 {
		return 1, nil
	}
	for i, e := range n.Entries {
		if e.IsValue() && bytes.Compare(key, e.Key) < 0 {
			// we don't need to recurse/iterate further
			return 0, nil
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
				return 0, fmt.Errorf("%w: can't compare key order recursively", ErrPartialTree)
			}
			order, err := e.Child.compareKey(key, markDirty)
			if err != nil {
				return 0, err
			}
			// lower than entire node
			if i == 0 && order < 0 {
				return -1, nil
			}
			// higher than entire node
			if i == len(n.Entries)-1 && order > 0 {
				return 1, nil
			}
			return 0, nil
		}
	}
	return 0, nil
}

// helper function, mostly for testing or development, which redusively inserts key/CID pairs into a `map[string]cid.Cid
func (n *Node) writeToMap(m map[string]cid.Cid) error {
	if m == nil {
		return fmt.Errorf("un-initialized map as an argument")
	}
	if n == nil {
		return fmt.Errorf("nil tree pointer")
	}
	for _, e := range n.Entries {
		if e.IsValue() {
			m[string(e.Key)] = *e.Value
		}
		if e.Child != nil {
			if err := e.Child.writeToMap(m); err != nil {
				return fmt.Errorf("failed to export MST structure as map: %w", err)
			}
		}
	}
	return nil
}

// Reads the value (CID) corresponding to the key. If key is not in the tree, returns (nil, nil).
//
// n: Node at top of sub-tree to operate on. Must not be nil.
// key: key or path being inserted. must not be empty/nil
// height: tree height corresponding to key. if a negative value is provided, will be computed; use -1 instead of 0 if height is not known
func (n *Node) getCID(key []byte, height int) (*cid.Cid, error) {
	if n.Stub {
		return nil, ErrPartialTree
	}
	if height < 0 {
		height = HeightForKey(key)
	}

	if height > n.Height {
		// key from a higher layer; key was not in tree
		return nil, nil
	}

	if height < n.Height {
		// look for a child node
		idx := n.findExistingChild(key)
		if idx >= 0 {
			if n.Entries[idx].Child == nil {
				return nil, fmt.Errorf("could not search for key: %w", ErrPartialTree)
			}
			return n.Entries[idx].Child.getCID(key, height)
		}
		// otherwise, not found
		return nil, nil
	}

	// search at this height
	idx := n.findExistingEntry(key)
	if idx >= 0 {
		return n.Entries[idx].Value, nil
	}

	// not found
	return nil, nil
}
