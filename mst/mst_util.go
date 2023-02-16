// Helpers for MST implementation. Following code split between mst.ts and util.ts in upstream Typescript implementation

package mst

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
)

// Used to determine the "depth" of keys in an MST.
// For atproto, the "fanout" is always 16, so we count "zeros" in chunks of
// 4-bits. Eg, a leading 0x00 byte is 2 "zeros".
// Typescript: leadingZerosOnHash(key, fanout) -> number
func leadingZerosOnHash(k string) int {
	hv := sha256.Sum256([]byte(k))

	total := 0
	for i := 0; i < len(hv); i++ {
		if hv[i] == 0x00 {
			total += 2
			continue
		} else if hv[i]&0xF0 == 0x00 {
			total += 1
		}
		break
	}
	return total
}

// Typescript: layerForEntries(entries, fanout) -> (number?)
func layerForEntries(entries []NodeEntry) int {
	var firstLeaf NodeEntry
	for _, e := range entries {
		if e.isLeaf() {
			firstLeaf = e
			break
		}
	}

	if firstLeaf.Kind == EntryUndefined {
		return -1
	}

	return leadingZerosOnHash(firstLeaf.Key)
}

// Typescript: deserializeNodeData(storage, data, layer)
func deserializeNodeData(ctx context.Context, cst cbor.IpldStore, nd *NodeData, layer int) ([]NodeEntry, error) {
	entries := []NodeEntry{}
	if nd.Left != nil {
		// Note: like Typescript, this is actually a lazy load
		entries = append(entries, NodeEntry{
			Kind: EntryTree,
			Tree: NewMST(cst, *nd.Left, nil, layer-1),
		})
	}

	var lastKey string
	for _, e := range nd.Entries {
		key := make([]byte, int(e.PrefixLen)+len(e.KeySuffix))
		copy(key, lastKey[:e.PrefixLen])
		copy(key[e.PrefixLen:], e.KeySuffix)

		entries = append(entries, NodeEntry{
			Kind: EntryLeaf,
			Key:  string(key),
			Val:  e.Val,
		})

		if e.Tree != nil {
			entries = append(entries, NodeEntry{
				Kind: EntryTree,
				Tree: NewMST(cst, *e.Tree, nil, layer-1),
				Key:  string(key),
			})
		}
		lastKey = string(key)
	}

	return entries, nil
}

// Typescript: serializeNodeData(entries) -> NodeData
func serializeNodeData(entries []NodeEntry) (*NodeData, error) {
	var data NodeData

	i := 0
	if len(entries) > 0 && entries[0].isTree() {
		i++

		ptr, err := entries[0].Tree.GetPointer(context.TODO())
		if err != nil {
			return nil, err
		}
		data.Left = &ptr
	}

	var lastKey string
	for i < len(entries) {
		leaf := entries[i]

		if !leaf.isLeaf() {
			return nil, fmt.Errorf("Not a valid node: two subtrees next to eachother (%d, %d)", i, len(entries))
		}
		i++

		var subtree *cid.Cid

		if i < len(entries) {
			next := entries[i]

			if next.isTree() {

				ptr, err := next.Tree.GetPointer(context.TODO())
				if err != nil {
					return nil, fmt.Errorf("getting subtree pointer: %w", err)
				}

				subtree = &ptr
				i++
			}
		}

		prefixLen := countPrefixLen(lastKey, leaf.Key)
		data.Entries = append(data.Entries, TreeEntry{
			PrefixLen: int64(prefixLen),
			KeySuffix: leaf.Key[prefixLen:],
			Val:       leaf.Val,
			Tree:      subtree,
		})

		lastKey = leaf.Key
	}

	return &data, nil
}

func min(a, b int) int {
	if a <= b {
		return a
	}
	return b
}

// how many leading chars are identical between the two strings?
// Typescript: countPrefixLen(a: string, b: string) -> number
func countPrefixLen(a, b string) int {
	count := min(len(a), len(b))
	for i := 0; i < count; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return count
}

// both computes *and* persists a tree entry; this is different from typescript
// implementation
// Typescript: cidForEntries(entries) -> CID
func cidForEntries(ctx context.Context, entries []NodeEntry, cst cbor.IpldStore) (cid.Cid, error) {
	nd, err := serializeNodeData(entries)
	if err != nil {
		return cid.Undef, fmt.Errorf("serializing new entries: %w", err)
	}

	return cst.Put(ctx, nd)
}
