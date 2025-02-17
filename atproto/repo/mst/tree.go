package mst

import (
	"context"
	"errors"
	"fmt"

	"github.com/ipfs/go-cid"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
)

// High-level API for an MST, as a decoded in-memory data structure.
//
// This might be an entire tree (all child nodes in-memory), or might be a partial tree with some nodes as CID links. Operations on the tree do not persist to any backing storage automatically.
//
// Errors when operating on the tree may leave the tree in a partially modified or invalid/corrupt state.
type Tree struct {
	Root *Node
	// TODO: have a blockstore.Blockstore for loading lazily?
}

var ErrInvalidKey = errors.New("bytestring not a valid MST key")

var ErrPartialTree = errors.New("MST is not complete")

var ErrInvalidTree = errors.New("invalid MST structure")

func NewEmptyTree() Tree {
	return Tree{
		Root: &Node{
			Dirty:  true,
			Height: 0,
		},
	}
}

// Adds a key/value to the tree, and returns any previously existing value (CID).
//
// Caller can inspect the previous value to determine if the behavior was a "creation" (key didn't exist), an "update" (key existed with a different value), or no-op (key existed with current value).
//
// key: key or path being inserted. must not be empty/nil
// val: CID value being inserted
func (t *Tree) Insert(key []byte, val cid.Cid) (*cid.Cid, error) {
	if !IsValidKey(key) {
		return nil, ErrInvalidKey
	}
	out, prev, err := t.Root.insert(key, val, -1)
	if err != nil {
		return nil, err
	}
	t.Root = out
	return prev, nil
}

// Removes key/value from the sub-tree provided. Return the previous CID value, if any. If key was not found, returns nil (which is not an error).
//
// key: key or path being inserted. must not be empty/nil
func (t *Tree) Remove(key []byte) (*cid.Cid, error) {
	if !IsValidKey(key) {
		return nil, ErrInvalidKey
	}
	out, prev, err := t.Root.remove(key, -1)
	if err != nil {
		return nil, err
	}
	t.Root = out
	return prev, nil
}

// Reads the value (CID) corresponding to the key.
//
// If key is not in the tree, returns nil, not an error.
//
// key: key or path being inserted. must not be empty/nil
func (t *Tree) Get(key []byte) (*cid.Cid, error) {
	if !IsValidKey(key) {
		return nil, ErrInvalidKey
	}
	return t.Root.getCID(key, -1)
}

// Creates a new Tree by loading key/value pairs from a map.
func LoadTreeFromMap(m map[string]cid.Cid) (*Tree, error) {
	if m == nil {
		return nil, fmt.Errorf("un-initialized map as an argument")
	}
	t := NewEmptyTree()
	var err error
	for key, val := range m {
		_, err = t.Insert([]byte(key), val)
		if err != nil {
			return nil, fmt.Errorf("unexpected failure to build MST structure: %w", err)
		}
	}
	return &t, nil
}

// Recursively walks the tree and writes key/value pairs to map `m`
//
// The map (`m`) is mutated in place (by reference); the map must be initialized before calling.
func (t *Tree) WriteToMap(m map[string]cid.Cid) error {
	if m == nil {
		return fmt.Errorf("un-initialized map as an argument")
	}
	if t.Root == nil {
		return fmt.Errorf("empty tree root")
	}
	return t.Root.writeToMap(m)
}

// Returns the overall root-node CID for the MST.
//
// If possible, lazily returned a known value. If necessary, recursively encodes tree nodes to compute CIDs.
//
// NOTE: will mark the tree "clean" (clear any dirty flags).
func (t *Tree) RootCID() (*cid.Cid, error) {
	if t.Root != nil && t.Root.Stub && !t.Root.Dirty && t.Root.CID != nil {
		return t.Root.CID, nil
	}
	return t.Root.writeBlocks(context.Background(), nil, true)
}

// If the tree contains no key/value pairs, returns true.
func (t *Tree) IsEmpty() bool {
	if t.Root == nil {
		return true
	}
	return t.Root.IsEmpty()
}

// Returns false if all nodes in the tree are available in-memory in decoded format; otherwise returns true. Does not consider record data, only MST nodes.
func (t *Tree) IsPartial() bool {
	if t.Root == nil {
		return true
	}
	return t.Root.IsPartial()
}

// Creates a deep copy of MST
func (t *Tree) Copy() Tree {
	return Tree{
		Root: t.Root.deepCopy(),
	}
}

func LoadTreeFromStore(ctx context.Context, bs blockstore.Blockstore, root cid.Cid) (*Tree, error) {
	n, err := loadNodeFromStore(ctx, bs, root)
	if err != nil {
		return nil, err
	}
	n.ensureHeights()
	return &Tree{
		Root: n,
	}, nil
}

// Walks the tree, encodes any "dirty" nodes as CBOR data, and writes that data as blocks to the provided blockstore. Returns root CID.
func (t *Tree) WriteDiffBlocks(ctx context.Context, bs blockstore.Blockstore) (*cid.Cid, error) {
	return t.Root.writeBlocks(ctx, bs, true)
}
