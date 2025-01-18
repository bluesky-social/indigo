package mst

import (
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

type Node struct {
	CID     *cid.Cid
	Entries []NodeEntry
	Height  int // negative for "unknown"
	Dirty   bool
}

// Represents an entry in an MST `Node`, which could either be a direct path/value entry, or a pointer do a child tree node. Note that these are *not* one-to-one with `EntryData`.
type NodeEntry struct {
	Path  string
	Value *cid.Cid
	Child *cid.Cid
}
