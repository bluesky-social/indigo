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
	Dirty bool // TODO: maybe invert as "clean" flag?
	// optionally, the last computed CID of this Node (when expressed as NodeData)
	CID *cid.Cid
}

// Represents an entry in an MST `Node`, which could either be a direct path/value entry, or a pointer do a child tree node. Note that these are *not* one-to-one with `EntryData`.
//
// Either the Key and Value fields should be non-zero; or the Child and/or ChildCID field should be non-zero.
// If ChildCID is present, but Child is not, then this is part of a "partial" tree.
type NodeEntry struct {
	Key   []byte
	Value *cid.Cid
	// TODO: probably a "dirty" flag here as well, to track key/value updates

	ChildCID *cid.Cid
	Child    *Node

	// tracks whether anything about this entry has changed since `Node` CID was computed
	Dirty bool
}

func NewEmptyNode() *Node {
	return &Node{
		Dirty:  true,
		Height: 0,
	}
}

func (n *Node) IsEmpty() bool {
	return len(n.Entries) == 0
}

// Checks if the sub-tree (this node, or any children, recursively) contains any CID references to nodes which are not present.
func (n *Node) IsPartial() bool {
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

// Looks for a "value" entry in the node with the exact key.
// Returns entry index if a matching entry is found; or -1 if not found
func findExistingEntry(n *Node, key []byte) int {
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
func findExistingChild(n *Node, key []byte) int {
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
func findInsertionIndex(n *Node, key []byte) (idx int, split bool, retErr error) {
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
			highest, err := getHighestKey(e.Child, false)
			if err != nil {
				return -1, false, err
			}
			if bytes.Compare(key, highest) > 0 {
				// key comes after this entire child sub-tree
				continue
			}
			lowest, err := getLowestKey(e.Child, false)
			if err != nil {
				return -1, false, err
			}
			if bytes.Compare(key, lowest) < 0 {
				// key comes before this entire child sub-tree
				return i, false, nil
			}
			// key falls inside this child sub-tree
			return i, true, nil
		}
	}

	// would need to be appended after
	return len(n.Entries), false, nil
}

// returns the lowest key along "left edge" of sub-tree
// TODO: convert this and "getHighest" to a CompareKey method which returns -1, 0, 1
func getLowestKey(n *Node, dirty bool) ([]byte, error) {
	if dirty == true {
		n.Dirty = true
	}
	e := n.Entries[0]
	if e.IsValue() {
		return e.Key, nil
	} else if e.IsChild() {
		if e.Child != nil {
			return getLowestKey(e.Child, dirty)
		} else {
			return nil, fmt.Errorf("can't determine key range of partial node")
		}
	}
	return nil, fmt.Errorf("invalid node entry")
}

// returns the lowest key along "left edge" of sub-tree
func getHighestKey(n *Node, dirty bool) ([]byte, error) {
	if dirty == true {
		n.Dirty = true
	}
	e := n.Entries[len(n.Entries)-1]
	if e.IsValue() {
		return e.Key, nil
	} else if e.IsChild() {
		if e.Child != nil {
			return getHighestKey(e.Child, dirty)
		} else {
			return nil, fmt.Errorf("can't determine key range of partial node")
		}
	}
	return nil, fmt.Errorf("invalid node entry")
}

func readNodeToMap(n *Node, m map[string]cid.Cid) error {
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
			if err := readNodeToMap(e.Child, m); err != nil {
				return fmt.Errorf("failed to export MST structure as map: %w", err)
			}
		}
	}
	return nil
}
