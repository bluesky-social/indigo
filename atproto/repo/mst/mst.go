package mst

import (
	"bytes"
	"errors"
	"fmt"
	"slices"

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
	Val       cid.Cid  `cborgen:"v"` // CID pointer at this path/key
	Tree      *cid.Cid `cborgen:"t"` // [nullable] pointer to lower-level subtree to the "right" of this path/key entry
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
}

func (n *Node) IsEmpty() bool {
	return len(n.Entries) == 0
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

// Adds a key/CID entry to a sub-tree defined by a Node. If a previous value existed, returns it.
//
// If the insert is a no-op (the key already existed with exact value), then the operation is a no-op, the tree is not marked dirty, and the val is returned as the 'prev' value.
//
// n: Node at top of sub-tree to operate on
// key: key or path being inserted. must not be empty/nil
// val: CID value being inserted
// height: tree height to insert at, derived from key. if a negative value is provided, will be computed; use -1 instead of 0 if height is not known
func Insert(n *Node, key []byte, val cid.Cid, height int) (*Node, *cid.Cid, error) {
	if height < 0 {
		height = HeightForKey(key)
	}

	if n == nil {
		return nil, nil, fmt.Errorf("operating on nil tree/node")
	}

	for height > n.Height {
		// if the new key is higher in the tree; will need to add a parent node, which may involve splitting this current node
		return insertParent(n, key, val, height)
	}

	// if key is lower on the tree, we need to descend first
	if height < n.Height {
		return insertChild(n, key, val, height)
	}

	// look for existing key
	idx := findExistingEntry(n, key)
	if idx >= 0 {
		e := n.Entries[idx]
		if *e.Value == val {
			// same value already exists; no-op
			return n, &val, nil
		}
		// update operation
		prev := e.Value
		n.Entries[idx].Value = &val
		n.Dirty = true
		return n, prev, nil
	}

	// insert new entry to this node
	idx, split, err := findInsertionIndex(n, key)
	if err != nil {
		return nil, nil, err
	}
	n.Dirty = true
	newEntry := NodeEntry{
		Key:   key,
		Value: &val,
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
	left, right, err := splitNode(e.Child, key)
	if err != nil {
		return nil, nil, err
	}
	// remove the existing entry, and replace with three new entries
	n.Entries = slices.Delete(n.Entries, idx, idx+1)
	n.Entries = slices.Insert(
		n.Entries,
		idx,
		NodeEntry{Child: left},
		newEntry,
		NodeEntry{Child: right},
	)
	return n, nil, nil
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

func splitNodeEntries(n *Node, idx int) (*Node, *Node, error) {
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

func splitNode(n *Node, key []byte) (*Node, *Node, error) {
	if n.IsEmpty() {
		// TODO: this feels defensive and could be removed
		return nil, nil, fmt.Errorf("tried to split an empty node")
	}

	idx, split, err := findInsertionIndex(n, key)
	if err != nil {
		return nil, nil, err
	}
	if !split {
		// simple split based on values
		return splitNodeEntries(n, idx)
	}

	// need to split recursively
	e := n.Entries[idx]
	lowerLeft, lowerRight, err := splitNode(e.Child, key)
	if err != nil {
		return nil, nil, err
	}
	left := &Node{
		Height:  n.Height,
		Dirty:   true,
		Entries: []NodeEntry{},
	}
	left.Entries = append(left.Entries, n.Entries[:idx]...)
	left.Entries = append(left.Entries, NodeEntry{Child: lowerLeft})
	right := &Node{
		Height:  n.Height,
		Dirty:   true,
		Entries: []NodeEntry{NodeEntry{Child: lowerRight}},
	}
	if idx+1 < len(n.Entries) {
		right.Entries = append(right.Entries, n.Entries[idx+1:]...)
	}
	return left, right, nil
}

// inserts a node "above" this node in tree, possibly splitting the current node
func insertParent(n *Node, key []byte, val cid.Cid, height int) (*Node, *cid.Cid, error) {
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
			}},
		}
	}
	// regular insertion will handle any necessary "split"
	return Insert(parent, key, val, height)
}

// inserts a node "below" this node in tree; either creating a new child entry or re-using an existing one
func insertChild(n *Node, key []byte, val cid.Cid, height int) (*Node, *cid.Cid, error) {
	// look for an existing child node which encompases the key, and use that
	idx := findExistingChild(n, key)
	if idx >= 0 {
		e := n.Entries[idx]
		if e.Child == nil {
			return nil, nil, fmt.Errorf("could not insert key: %w", ErrPartialTree)
		}
		newChild, prev, err := Insert(e.Child, key, val, height)
		if err != nil {
			return nil, nil, err
		}
		if prev != nil && *prev == val {
			// no-op
			return n, &val, nil
		}
		n.Dirty = true
		n.Entries[idx].Child = newChild
		return n, prev, nil
	}

	// insert a new child node. this might be recursive if the child is not a *direct* child
	idx, split, err := findInsertionIndex(n, key)
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
	newChild, _, err = Insert(newChild, key, val, height)
	if err != nil {
		return nil, nil, err
	}
	newEntry := NodeEntry{
		Child: newChild,
	}
	if idx == len(n.Entries) {
		n.Entries = append(n.Entries, newEntry)
	} else {
		n.Entries = slices.Insert(n.Entries, idx, newEntry)
	}
	return n, nil, nil
}

// Removes key/value from the sub-tree provided, returning a new tree, and the previous CID value. If key is not found, returns unmodified subtree, and nil for the returned CID.
//
// n: Node at top of sub-tree to operate on. Must not be nil.
// key: key or path being inserted. must not be empty/nil
// height: tree height corresponding to key. if a negative value is provided, will be computed; use -1 instead of 0 if height is not known
func Remove(n *Node, key []byte, height int) (*Node, *cid.Cid, error) {
	if height < 0 {
		height = HeightForKey(key)
	}

	if height > n.Height {
		// removing a key from a higher layer; key was not in tree
		return n, nil, nil
	}

	if height < n.Height {
		return removeChild(n, key, height)
	}

	// look at this level
	for i, e := range n.Entries {
		if !e.IsValue() {
			continue
		}
		if bytes.Equal(key, e.Key) {
			// found it! remove from list
			var prev *cid.Cid
			n.Dirty = true
			prev = e.Value
			n.Entries = slices.Delete(n.Entries, i, i+1)
			return n, prev, nil
		}
	}

	// key not found
	return n, nil, nil
}

// internal helper
func removeChild(n *Node, key []byte, height int) (*Node, *cid.Cid, error) {
	// XXX: handle situation of merging entries at this level if child is now empty

	// look for a child
	childIndex := -1
	for i, e := range n.Entries {
		if e.IsValue() {
			if bytes.Compare(key, e.Key) > 0 {
				break
			}
		}
		if e.IsChild() {
			childIndex = i
		}
	}
	if childIndex < 0 {
		// no child pointer; key not in tree
		return n, nil, nil
	}
	if n.Entries[childIndex].Child == nil {
		// partial node, can't recurse
		return nil, nil, fmt.Errorf("could not remove key: %w", ErrPartialTree)
	}
	newChild, prev, err := Remove(n.Entries[childIndex].Child, key, height)
	if err != nil {
		return nil, nil, err
	}
	if prev == nil {
		// nothing changed
		return n, nil, nil
	}
	// we are mutating this entry
	n.Dirty = true
	// if new child is empty, remove it from entry list
	if len(newChild.Entries) == 0 {
		// TODO: do we need to check for len(n.Entries) for this slice?
		n.Entries = slices.Delete(n.Entries, childIndex, childIndex+1)
	}
	// if *this* node is now empty, return empty tree
	if len(n.Entries) == 0 {
		emptyNode := NewEmptyTree()
		return emptyNode, prev, nil
	}
	// otherwise, return this node
	return n, prev, nil
}

// Reads the value (CID) corresponding to the key. If key is not in the tree, returns (nil, nil).
//
// n: Node at top of sub-tree to operate on. Must not be nil.
// key: key or path being inserted. must not be empty/nil
// height: tree height corresponding to key. if a negative value is provided, will be computed; use -1 instead of 0 if height is not known
func Get(n *Node, key []byte, height int) (*cid.Cid, error) {
	if height < 0 {
		height = HeightForKey(key)
	}

	if height > n.Height {
		// key from a higher layer; key was not in tree
		return nil, nil
	}

	if height < n.Height {
		// look for a child node
		idx := findExistingChild(n, key)
		if idx >= 0 {
			if n.Entries[idx].Child == nil {
				return nil, fmt.Errorf("could not search for key: %w", ErrPartialTree)
			}
			return Get(n.Entries[idx].Child, key, height)
		}
	}

	// search at this height
	idx := findExistingEntry(n, key)
	if idx >= 0 {
		return n.Entries[idx].Value, nil
	}

	// not found
	return nil, nil
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
