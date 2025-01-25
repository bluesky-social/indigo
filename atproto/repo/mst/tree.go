package mst

import (
	"errors"
	"fmt"

	"github.com/ipfs/go-cid"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
)

type Tree struct {
	Root   *Node
	Blocks blockstore.Blockstore
}

var ErrPartialTree = errors.New("MST is not complete")

var ErrKeyNotFound = errors.New("MST does not contain key")

var ErrInvalidKey = errors.New("bytestring not a valid MST key")

var ErrInvalidTree = errors.New("invalid MST structure")

// XXX:
func NewEmptyTree() Tree {
	return Tree{
		Root: &Node{
			Dirty:  true,
			Height: 0,
		},
	}
}

// Adds a key/CID entry to a sub-tree defined by a Node. If a previous value existed, returns it.
//
// If the insert is a no-op (the key already existed with exact value), then the operation is a no-op, and the val is returned as the 'prev' value.
//
// key: key or path being inserted. must not be empty/nil
// val: CID value being inserted
func (t *Tree) Insert(key []byte, val cid.Cid) (*cid.Cid, error) {
	out, prev, err := nodeInsert(t.Root, key, val, -1)
	if err != nil {
		return nil, err
	}
	t.Root = out
	return prev, nil
}

// Removes key/value from the sub-tree provided. Return the previous CID value; if key is not found, returns nil.
//
// key: key or path being inserted. must not be empty/nil
func (t *Tree) Remove(key []byte) (*cid.Cid, error) {
	out, prev, err := nodeRemove(t.Root, key, -1)
	if err != nil {
		return nil, err
	}
	t.Root = out
	return prev, nil
}

// Reads the value (CID) corresponding to the key. If key is not in the tree, returns (nil, nil).
//
// key: key or path being inserted. must not be empty/nil
func (t *Tree) Get(key []byte) (*cid.Cid, error) {
	return nodeGet(t.Root, key, -1)
}

// XXX:
func NewTreeFromMap(m map[string]cid.Cid) (*Tree, error) {
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
func (t *Tree) ReadToMap(m map[string]cid.Cid) error {
	if m == nil {
		return fmt.Errorf("un-initialized map as an argument")
	}
	if t.Root == nil {
		return fmt.Errorf("empty tree root")
	}
	return readNodeToMap(t.Root, m)
}

func (t *Tree) RootCID() (*cid.Cid, error) {
	return NodeCID(t.Root)
}
