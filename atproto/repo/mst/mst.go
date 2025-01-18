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
	// optionally, the last computed CID of this Node (when expressed as NodeData)
	CID *cid.Cid
	// array of key/value pairs and pointers to child nodes. entry arrays must always be in correct/valid order at any point in time: sorted by 'key', and at most one 'pointer' entry between 'value' entries.
	Entries []NodeEntry
	// "height" or "layer" of MST tree this node is at (with zero at the "bottom" and root/top of tree the "highest")
	Height int
	// if true, the cached CID of this node is out of date
	Dirty bool // TODO: maybe invert as "clean" flag?
}

// Represents an entry in an MST `Node`, which could either be a direct path/value entry, or a pointer do a child tree node. Note that these are *not* one-to-one with `EntryData`.
//
// Either the Key and Value fields should be non-zero; or the Child and/or ChildCID field should be non-zero.
// If ChildCID is present, but Child is not, then this is part of a "partial" tree.
type NodeEntry struct {
	Key   []byte
	Value *cid.Cid

	ChildCID *cid.Cid
	Child    *Node
}

// Returns true if this is a key/value entry
func (e *NodeEntry) IsValue() bool {
	if len(e.Key) > 0 && e.Value != nil {
		return true
	}
	return false
}

// Returns true if this is a pointer to a child on a lower level
func (e *NodeEntry) IsPointer() bool {
	if e.Child != nil || e.ChildCID != nil {
		return true
	}
	return false
}

var ErrPartialTree = errors.New("MST is not complete")

var ErrKeyNotFound = errors.New("MST does not contain key")

var ErrInvalidTree = errors.New("invalid MST structure")

// Adds a key/CID entry to a sub-tree defined by a Node. If a previous value existed, returns it.
//
// n: Node at top of sub-tree to operate on. Can be nil, in which case a new Node is created.
// key: key or path being inserted. must not be empty/nil
// val: CID value being inserted
// height: tree height to insert at, derived from key. if a negative value is provided, will be computed; use -1 instead of 0 if height is not known
//
// TODO(correctness): if key already existing with given val, can we avoid dirtying the tree? or is that too complex
func Insert(n *Node, key []byte, val cid.Cid, height int) (*Node, *cid.Cid, error) {
	if height < 0 {
		height = HeightForKey(key)
	}

	if n == nil {
		n = &Node{
			Height: height,
			Dirty:  true,
		}
	}

	for height > n.Height {
		// if the new key is higher in the tree; will need to add a parent node, which may involve splitting this current node
		// TODO(perf): if current node is empty, just replace with correct height
		newNode := Node{
			CID:   nil,
			Dirty: true,
			Entries: []NodeEntry{NodeEntry{
				Child: n,
			}},
			Height: n.Height + 1,
		}
		n = &newNode
	}

	// if key is lower on the tree, we need to descend first
	if height < n.Height {

		if len(n.Entries) == 0 {
			// if there is nothing at this level, we need to insert a child pointer. this process may be recursive.
			child := &Node{
				Height: n.Height - 1,
				Dirty:  true,
			}
			child, _, err := Insert(child, key, val, height)
			if err != nil {
				return nil, nil, err
			}
			n.Entries = []NodeEntry{
				NodeEntry{Child: child},
			}
			return n, nil, nil
		}
		// look for slot where child exists (or would go)
		childIndex := 0
		for i, e := range n.Entries {
			if e.IsValue() {
				if bytes.Compare(key, e.Key) < 0 {
					break
				}
			}
			childIndex = i
		}
		if n.Entries[childIndex].IsPointer() {
			// there already exists a child entry to work with
			if n.Entries[childIndex].Child == nil {
				return nil, nil, fmt.Errorf("could not insert key: %w", ErrPartialTree)
			}
			newChild, prev, err := Insert(n.Entries[childIndex].Child, key, val, height)
			if err != nil {
				return nil, nil, err
			}
			if prev == nil {
				return n, nil, nil
			}
			n.Dirty = true
			n.Entries[childIndex].Child = newChild
			return n, prev, nil
		}

		// we need to insert a new child entry and node
		n.Dirty = true
		if !n.Entries[childIndex].IsValue() || bytes.Equal(key, n.Entries[childIndex].Key) {
			// TODO: better error
			return nil, nil, fmt.Errorf("unexpected bad MST structure")
		}
		child := &Node{
			Height: n.Height - 1,
			Dirty:  true,
		}
		child, _, err := Insert(child, key, val, height)
		if err != nil {
			return nil, nil, err
		}
		entry := NodeEntry{Child: child}
		if bytes.Compare(key, n.Entries[childIndex].Key) > 0 {
			// inserting after
			n.Entries = slices.Insert(n.Entries, childIndex+1, entry)
		} else {
			// inserting before
			n.Entries = slices.Insert(n.Entries, childIndex, entry)
		}
		return n, nil, nil
	}

	// we are at the correct height, and can just insert
	if height != n.Height {
		return nil, nil, fmt.Errorf("unexpected MST height mismatch")
	}

	n.Dirty = true

	if len(n.Entries) == 0 {
		// simply insert an entry
		n.Entries = []NodeEntry{NodeEntry{
			Key:   key,
			Value: &val,
		}}
		return n, nil, nil
	}

	// look for slot where entry exists (or would go)
	insertIndex := 0
	for i, e := range n.Entries {
		if e.IsValue() {
			if bytes.Equal(key, e.Key) {
				if *e.Value == val {
					// no-op
					return n, nil, nil
				}
				prev := *e.Value
				n.Entries[i].Value = &val
				return n, &prev, nil
			}
			if bytes.Compare(key, e.Key) > 0 {
				break
			}
		}
		insertIndex = i
	}

	entry := NodeEntry{
		Key:   key,
		Value: &val,
	}

	if n.Entries[insertIndex].IsValue() {
		if bytes.Compare(n.Entries[insertIndex].Key, key) > 0 {
			// insert after
			n.Entries = slices.Insert(n.Entries, insertIndex+1, entry)
			return n, nil, nil
		} else {
			// insert before
			n.Entries = slices.Insert(n.Entries, insertIndex, entry)
			return n, nil, nil
		}
	} else if n.Entries[insertIndex].IsPointer() {
		// need to descend tree; and may even need to split
		// XXX:
		n.Entries = slices.Insert(n.Entries, insertIndex+1, entry)
		return n, nil, nil
	}

	// append before
	n.Entries = slices.Insert(n.Entries, insertIndex, entry)
	return n, nil, nil
}

func NewEmptyTree() *Node {
	return &Node{
		Dirty:  true,
		Height: 0,
	}
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
		// look for a child
		childIndex := -1
		for i, e := range n.Entries {
			if e.IsValue() {
				if bytes.Compare(key, e.Key) > 0 {
					break
				}
			}
			if e.IsPointer() {
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
			n.Entries = slices.Delete(n.Entries, childIndex, childIndex)
		}
		// if *this* node is now empty, return empty tree
		if len(n.Entries) == 0 {
			emptyNode := NewEmptyTree()
			return emptyNode, prev, nil
		}
		// otherwise, return this node
		return n, prev, nil
	}

	// look at this level
	for i, e := range n.Entries {
		var prev *cid.Cid
		if e.IsValue() && bytes.Equal(key, e.Key) {
			n.Dirty = true
			prev = e.Value
			// remove from entry list
			n.Entries = append(n.Entries[:i], n.Entries[i+1:]...)
		}
		if len(n.Entries) == 0 {
			// if *this* node is now empty, return empty tree
			emptyNode := NewEmptyTree()
			return emptyNode, prev, nil
		}
		return n, prev, nil
	}

	// key not found
	return n, nil, nil
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
		// removing a key from a higher layer; key was not in tree
		return nil, nil
	}

	if height < n.Height {
		// look for a child
		childIndex := -1
		for i, e := range n.Entries {
			if e.IsValue() {
				if bytes.Compare(key, e.Key) > 0 {
					break
				}
			}
			if e.IsPointer() {
				childIndex = i
			}
		}
		if childIndex < 0 {
			// no child pointer; key not in tree
			return nil, nil
		}
		if n.Entries[childIndex].Child == nil {
			// partial node, can't recurse
			return nil, fmt.Errorf("could not search for key: %w", ErrPartialTree)
		}
		return Get(n.Entries[childIndex].Child, key, height)
	}

	// look at this height
	for _, e := range n.Entries {
		if e.IsValue() && bytes.Equal(e.Key, key) {
			return e.Value, nil
		}
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
	var n *Node
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

// TODO: "walk tree" helper (?)
// TODO: "hydrate" helper which pulls in blocks by CID (?)
