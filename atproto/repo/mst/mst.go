package mst

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/ipfs/go-cid"
)

// CBOR serialization struct for a MST tree node. MST tree node as gets serialized to CBOR. Note that the CBOR fields are all single-character.
type NodeData struct {
	Left    *cid.Cid    `cborgen:"l"` // [nullable] pointer to lower-level subtree to the "left" of this path/key
	Entries []EntryData `cborgen:"e"` // ordered list of entries at this node
}

// CBOR serialization struct for a single entry within a `NodeData` entry list.
type EntryData struct {
	PrefixLen int64    `cborgen:"p"` // count of characters shared with previous path/key in tree
	KeySuffix []byte   `cborgen:"k"` // remaining part of path/key (appended to "previous key")
	Value     cid.Cid  `cborgen:"v"` // CID pointer at this path/key
	Right     *cid.Cid `cborgen:"t"` // [nullable] pointer to lower-level subtree to the "right" of this path/key entry
}

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

var ErrPartialTree = errors.New("MST is not complete")

var ErrKeyNotFound = errors.New("MST does not contain key")

var ErrInvalidKey = errors.New("bytestring not a valid MST key")

var ErrInvalidTree = errors.New("invalid MST structure")

func NewEmptyTree() *Node {
	return &Node{
		Dirty:  true,
		Height: 0,
	}
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
			if bytes.Compare(key, getHighestKey(e.Child)) > 0 {
				// key comes after this entire child sub-tree
				continue
			}
			if bytes.Compare(key, getLowestKey(e.Child)) < 0 {
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
func getLowestKey(n *Node) []byte {
	// TODO: make this a method on Node, and return an error (?)
	if len(n.Entries) == 0 {
		// TODO: throw error on this case?
		return nil
	}
	e := n.Entries[0]
	if e.IsValue() {
		return e.Key
	} else if e.IsChild() {
		if e.Child != nil {
			return getLowestKey(e.Child)
		} else {
			// TODO: throw error on this case?
		}
	}
	// TODO: throw error on this case?
	return nil
}

// returns the lowest key along "left edge" of sub-tree
func getHighestKey(n *Node) []byte {
	if len(n.Entries) == 0 {
		// TODO: throw error on this case?
		return nil
	}
	e := n.Entries[len(n.Entries)-1]
	if e.IsValue() {
		return e.Key
	} else if e.IsChild() {
		if e.Child != nil {
			return getHighestKey(e.Child)
		} else {
			// TODO: throw error on this case?
		}
	}
	// TODO: throw error on this case?
	return nil
}

func NewTreeFromMap(m map[string]cid.Cid) (*Node, error) {
	if m == nil {
		return nil, fmt.Errorf("un-initialized map as an argument")
	}
	n := NewEmptyTree()
	var err error
	for key, val := range m {
		n, _, err = Insert(n, []byte(key), val, -1)
		if err != nil {
			return nil, fmt.Errorf("unexpected failure to build MST structure: %w", err)
		}
	}
	return n, nil
}

// Recursively walks sub-tree (`Node` and all children) and writes key/value pairs to map `m`
//
// The map (`m`) is muted in place (by reference); the map must be initialized before calling.
func ReadTreeToMap(n *Node, m map[string]cid.Cid) error {
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
			if err := ReadTreeToMap(e.Child, m); err != nil {
				return fmt.Errorf("failed to export MST structure as map: %w", err)
			}
		}
	}
	return nil
}
