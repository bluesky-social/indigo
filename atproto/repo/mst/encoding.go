package mst

import (
	"bytes"
	"context"
	"fmt"
	"io"

	bf "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	ipld "github.com/ipfs/go-ipld-format"
	"github.com/multiformats/go-multihash"
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

// Encodes a single `NodeData` struct as CBOR bytes. Does not recursively encode or update children.
func (d *NodeData) Bytes() ([]byte, *cid.Cid, error) {
	buf := new(bytes.Buffer)
	if err := d.MarshalCBOR(buf); err != nil {
		return nil, nil, err
	}
	b := buf.Bytes()
	builder := cid.NewPrefixV1(cid.DagCBOR, multihash.SHA2_256)
	c, err := builder.Sum(b)
	if err != nil {
		return nil, nil, err
	}
	return b, &c, nil
}

// Parses CBOR bytes in to `NodeData` struct
func NodeDataFromCBOR(r io.Reader) (*NodeData, error) {
	var nd NodeData
	if err := nd.UnmarshalCBOR(r); err != nil {
		return nil, err
	}
	// TODO: verify CID type, and "non-empty" here?
	return &nd, nil
}

// Transforms `Node` struct to `NodeData`, which is the format used for encoding to CBOR.
//
// Will panic if any entries are missing a CID (must compute those first)
func (n *Node) NodeData() NodeData {
	d := NodeData{
		Left:    nil,
		Entries: []EntryData{}, // TODO perf: pre-allocate an array
	}

	prevKey := []byte{}
	for i, e := range n.Entries {
		if i == 0 && e.IsChild() {
			d.Left = e.ChildCID
			continue
		}
		if e.IsChild() {
			if len(d.Entries) == 0 {
				panic("malformed tree node") // TODO: return error?
			}
			d.Entries[len(d.Entries)-1].Right = e.ChildCID
		}
		if e.IsValue() {
			idx := int64(CountPrefixLen(prevKey, e.Key))
			d.Entries = append(d.Entries, EntryData{
				PrefixLen: idx,
				KeySuffix: e.Key[idx:],
				Value:     *e.Value,
				Right:     nil,
			})
			prevKey = e.Key
		}
	}
	return d
}

// Tansforms an encoded `NodeData` to `Node` data structure format.
//
// c: optional CID argument for the CID of the CBOR representation of the NodeData
func (d *NodeData) Node(c *cid.Cid) Node {
	height := -1
	n := Node{
		CID:     c,
		Dirty:   c == nil,
		Entries: []NodeEntry{}, // TODO: pre-allocate
	}

	if d.Left != nil {
		n.Entries = append(n.Entries, NodeEntry{ChildCID: d.Left})
	}

	var prevKey []byte
	for _, e := range d.Entries {
		// TODO perf: pre-allocate
		key := []byte{}
		key = append(key, prevKey[:e.PrefixLen]...)
		key = append(key, e.KeySuffix...)
		n.Entries = append(n.Entries, NodeEntry{
			Key:   key,
			Value: &e.Value,
		})
		prevKey = key
		if height < 0 {
			height = HeightForKey(key)
		}

		if e.Right != nil {
			n.Entries = append(n.Entries, NodeEntry{
				ChildCID: e.Right,
			})
		}
	}

	// TODO: height doesn't get set properly if this is an intermediate node; we rely on `EnsureHeights` getting called to fix that
	n.Height = height
	return n
}

// TODO: this feels like a hack, and easy to forget
func (n *Node) ensureHeights() {
	if n.Height <= 0 {
		return
	}
	for _, e := range n.Entries {
		if e.Child != nil {
			if n.Height > 0 && e.Child.Height < 0 {
				e.Child.Height = n.Height - 1
			}
			e.Child.ensureHeights()
		}
	}
}

// Recursively encodes sub-tree, optionally writing to blockstore. Returns root CID.
//
// This method will not error if tree is partial.
//
// bs: is an optional blockstore; if it is nil, blocks will not be written.
// onlyDirty: is an optional blockstore; if it is nil, blocks will not be written.
func (n *Node) writeBlocks(ctx context.Context, bs blockstore.Blockstore, onlyDirty bool) (*cid.Cid, error) {
	if n == nil || n.Stub {
		return nil, fmt.Errorf("%w: nil tree node", ErrInvalidTree)
	}
	if onlyDirty && !n.Dirty && n.CID != nil {
		return n.CID, nil
	}

	// walk all children first
	for i, e := range n.Entries {
		if e.IsValue() && e.Dirty {
			// TODO: should we actually clear this here?
			e.Dirty = false
		}
		if !e.IsChild() {
			continue
		}
		if e.Child != nil && (e.Dirty || e.Child.Dirty || !onlyDirty) {
			cc, err := e.Child.writeBlocks(ctx, bs, onlyDirty)
			if err != nil {
				return nil, err
			}
			n.Entries[i].ChildCID = cc
			n.Entries[i].Dirty = false
		}
	}

	// compute this block
	nd := n.NodeData()
	b, c, err := nd.Bytes()
	if err != nil {
		return nil, err
	}

	n.CID = c
	n.Dirty = false

	if bs != nil {
		blk, err := bf.NewBlockWithCid(b, *c)
		if err != nil {
			return nil, err
		}
		if err := bs.Put(ctx, blk); err != nil {
			return nil, err
		}
	}
	return c, nil
}

func loadNodeFromStore(ctx context.Context, bs blockstore.Blockstore, ref cid.Cid) (*Node, error) {
	block, err := bs.Get(ctx, ref)
	if err != nil {
		return nil, err
	}

	nd, err := NodeDataFromCBOR(bytes.NewReader(block.RawData()))
	if err != nil {
		return nil, err
	}

	n := nd.Node(&ref)

	for i, e := range n.Entries {
		if e.IsChild() {
			child, err := loadNodeFromStore(ctx, bs, *e.ChildCID)
			if err != nil && ipld.IsNotFound(err) {
				// allow "partial" trees
				continue
			}
			if err != nil {
				return nil, err
			}
			n.Entries[i].Child = child
			// NOTE: this is kind of a hack
			if n.Height == -1 && child.Height >= 0 {
				n.Height = child.Height + 1
			}
		}
	}

	return &n, nil
}
