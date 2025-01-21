package mst

import (
	"bytes"
	"fmt"
	"io"

	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
)

// Recursively calculates the root CID
func NodeCID(n *Node) (*cid.Cid, error) {
	if n == nil {
		return nil, fmt.Errorf("nil tree") // TODO: wrap an error?
	}
	if !n.Dirty && n.CID != nil {
		return n.CID, nil
	}

	// ensure all children are computed
	for i, e := range n.Entries {
		if !e.IsChild() {
			continue
		}
		// TODO: better efficiency here? track dirty on NodeEntry?
		if e.Child != nil {
			cc, err := NodeCID(e.Child)
			if err != nil {
				return nil, err
			}
			n.Entries[i].ChildCID = cc
		}
	}

	nd := n.NodeData()

	b, err := nd.CBOR()
	if err != nil {
		return nil, err
	}

	builder := cid.NewPrefixV1(cid.DagCBOR, multihash.SHA2_256)
	c, err := builder.Sum(b)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// Returns this node as CBOR bytes
func (d *NodeData) CBOR() ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := d.MarshalCBOR(buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func NodeDataFromCBOR(r io.Reader) (*NodeData, error) {
	var nd NodeData
	if err := nd.UnmarshalCBOR(r); err != nil {
		return nil, err
	}
	return &nd, nil
}

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

// c: CID argument for the CID of the CBOR representation of the NodeData (if known)
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

	// XXX: height doesn't get set properly if this is an intermediate node
	n.Height = height
	return n
}
