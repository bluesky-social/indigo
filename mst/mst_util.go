// Helpers for MST implementation. Following code split between mst.ts and util.ts in upstream Typescript implementation

package mst

import (
	"context"
	"fmt"
	"strings"
	"unsafe"

	"github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
	sha256 "github.com/minio/sha256-simd"
)

// Used to determine the "depth" of keys in an MST.
// For atproto, repo v2, the "fanout" is always 4, so we count "zeros" in
// chunks of 2-bits. Eg, a leading 0x00 byte is 4 "zeros".
// Typescript: leadingZerosOnHash(key, fanout) -> number
func leadingZerosOnHash(key string) int {
	var b []byte
	if len(key) > 0 {
		b = unsafe.Slice(unsafe.StringData(key), len(key))
	}
	return leadingZerosOnHashBytes(b)
}

func leadingZerosOnHashBytes(key []byte) (total int) {
	hv := sha256.Sum256(key)
	for _, b := range hv {
		if b&0xC0 != 0 {
			// Common case. No leading pair of zero bits.
			break
		}
		if b == 0x00 {
			total += 4
			continue
		}
		if b&0xFC == 0x00 {
			total += 3
		} else if b&0xF0 == 0x00 {
			total += 2
		} else {
			total += 1
		}
		break
	}
	return total
}

// Typescript: layerForEntries(entries, fanout) -> (number?)
func layerForEntries(entries []nodeEntry) int {
	var firstLeaf nodeEntry
	for _, e := range entries {
		if e.isLeaf() {
			firstLeaf = e
			break
		}
	}

	if firstLeaf.Kind == entryUndefined {
		return -1
	}

	return leadingZerosOnHash(firstLeaf.Key)
}

// Typescript: deserializeNodeData(storage, data, layer)
func deserializeNodeData(ctx context.Context, cst cbor.IpldStore, nd *NodeData, layer int) ([]nodeEntry, error) {
	entries := []nodeEntry{}
	if nd.Left != nil {
		// Note: like Typescript, this is actually a lazy load
		entries = append(entries, nodeEntry{
			Kind: entryTree,
			Tree: createMST(cst, *nd.Left, nil, layer-1),
		})
	}

	var lastKey string
	var keyb []byte // re-used between entries
	for _, e := range nd.Entries {
		if keyb == nil {
			keyb = make([]byte, 0, int(e.PrefixLen)+len(e.KeySuffix))
		}
		keyb = append(keyb[:0], lastKey[:e.PrefixLen]...)
		keyb = append(keyb, e.KeySuffix...)

		keyStr := string(keyb)
		err := ensureValidMstKey(keyStr)
		if err != nil {
			return nil, err
		}

		entries = append(entries, nodeEntry{
			Kind: entryLeaf,
			Key:  keyStr,
			Val:  e.Val,
		})

		if e.Tree != nil {
			entries = append(entries, nodeEntry{
				Kind: entryTree,
				Tree: createMST(cst, *e.Tree, nil, layer-1),
				Key:  keyStr,
			})
		}
		lastKey = keyStr
	}

	return entries, nil
}

// Typescript: serializeNodeData(entries) -> NodeData
func serializeNodeData(entries []nodeEntry) (*NodeData, error) {
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
			return nil, fmt.Errorf("Not a valid node: two subtrees next to each other (%d, %d)", i, len(entries))
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

		err := ensureValidMstKey(leaf.Key)
		if err != nil {
			return nil, err
		}

		prefixLen := countPrefixLen(lastKey, leaf.Key)
		data.Entries = append(data.Entries, TreeEntry{
			PrefixLen: int64(prefixLen),
			KeySuffix: []byte(leaf.Key)[prefixLen:],
			Val:       leaf.Val,
			Tree:      subtree,
		})

		lastKey = leaf.Key
	}

	return &data, nil
}

// how many leading bytes are identical between the two strings?
// Typescript: countPrefixLen(a: string, b: string) -> number
func countPrefixLen(a, b string) int {
	// This pattern avoids panicindex calls, as the Go compiler's prove pass can
	// convince itself that neither a[i] nor b[i] are ever out of bounds.
	var i int
	for i = 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return i
}

// both computes *and* persists a tree entry; this is different from typescript
// implementation
// Typescript: cidForEntries(entries) -> CID
func cidForEntries(ctx context.Context, entries []nodeEntry, cst cbor.IpldStore) (cid.Cid, error) {
	nd, err := serializeNodeData(entries)
	if err != nil {
		return cid.Undef, fmt.Errorf("serializing new entries: %w", err)
	}

	return cst.Put(ctx, nd)
}

// keyHasAllValidChars reports whether s matches
// the regexp /^[a-zA-Z0-9_:.-]+$/ without using regexp,
// which is slower.
func keyHasAllValidChars(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		b := s[i]
		if 'a' <= b && b <= 'z' ||
			'A' <= b && b <= 'Z' ||
			'0' <= b && b <= '9' {
			continue
		}
		switch b {
		case '_', ':', '.', '-':
			continue
		default:
			return false
		}
	}
	return true
}

// Typescript: isValidMstKey(str)
func isValidMstKey(s string) bool {
	if len(s) > 256 || strings.Count(s, "/") != 1 {
		return false
	}
	a, b, _ := strings.Cut(s, "/")
	return len(b) > 0 &&
		keyHasAllValidChars(a) &&
		keyHasAllValidChars(b)
}

// Typescript: ensureValidMstKey(str)
func ensureValidMstKey(s string) error {
	if !isValidMstKey(s) {
		return fmt.Errorf("Not a valid MST key: %s", s)
	}
	return nil
}
