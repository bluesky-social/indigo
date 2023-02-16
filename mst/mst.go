// Merkle Search Tree (MST) implementation for atproto.
// This implementation is a port of the Typescript implementation in the
// `atproto` git repo.
//
// The hash function is SHA-256, and "fanout=16", which corresponds to counting
// "leading zeros" for tree level in 4-bit chunks.

package mst

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/ipfs/go-cid"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	cbor "github.com/ipfs/go-ipld-cbor"
	cbg "github.com/whyrusleeping/cbor-gen"
)

type MerkleSearchTree struct {
	cst cbor.IpldStore

	entries  []NodeEntry
	layer    int
	pointer  cid.Cid
	validPtr bool
}

func NewMST(cst cbor.IpldStore, ptr cid.Cid, entries []NodeEntry, layer int) *MerkleSearchTree {
	mst := &MerkleSearchTree{
		cst:      cst,
		pointer:  ptr,
		layer:    layer,
		entries:  entries,
		validPtr: ptr.Defined(),
	}

	return mst
}

func LoadMST(cst cbor.IpldStore, root cid.Cid) *MerkleSearchTree {
	return NewMST(cst, root, nil, -1)
}

func (mst *MerkleSearchTree) newTree(entries []NodeEntry) *MerkleSearchTree {
	if entries == nil {
		panic("nil entries passed to newTree")
	}
	return NewMST(mst.cst, cid.Undef, entries, mst.layer)
}

func (mst *MerkleSearchTree) GetPointer(ctx context.Context) (cid.Cid, error) {
	if mst.validPtr {
		return mst.pointer, nil
	}

	entries, err := mst.getEntries(ctx)
	if err != nil {
		return cid.Undef, err
	}

	for _, e := range entries {
		if e.isTree() {
			if !e.Tree.validPtr {
				_, err := e.Tree.GetPointer(ctx)
				if err != nil {
					return cid.Undef, err
				}
			}
		}
	}

	nptr, err := cidForEntries(ctx, entries, mst.cst)
	if err != nil {
		return cid.Undef, err
	}
	mst.pointer = nptr
	mst.validPtr = true

	return mst.pointer, nil
}

func create(ctx context.Context, cst cbor.IpldStore, entries []NodeEntry, layer int) (*MerkleSearchTree, error) {
	var ptr cid.Cid
	return NewMST(cst, ptr, entries, layer), nil
}

func cborGet(ctx context.Context, bs blockstore.Blockstore, c cid.Cid, out cbg.CBORUnmarshaler) error {
	blk, err := bs.Get(ctx, c)
	if err != nil {
		return err
	}

	if err := out.UnmarshalCBOR(bytes.NewReader(blk.RawData())); err != nil {
		return err
	}

	return nil
}

const (
	EntryUndefined = 0
	EntryLeaf      = 1
	EntryTree      = 2
)

type NodeEntry struct {
	Kind int
	Key  string
	Val  cid.Cid
	Tree *MerkleSearchTree
}

func treeEntry(t *MerkleSearchTree) NodeEntry {
	return NodeEntry{
		Kind: EntryTree,
		Tree: t,
	}
}

func (ne NodeEntry) isTree() bool {
	return ne.Kind == EntryTree
}

func (ne NodeEntry) isLeaf() bool {
	return ne.Kind == EntryLeaf
}

func (ne NodeEntry) isUndefined() bool {
	return ne.Kind == EntryUndefined
}

type TreeEntry struct {
	PrefixLen int64    `cborgen:"p"` // count of characters shared with previous path/key in tree
	KeySuffix string   `cborgen:"k"` // remaining part of path/key (appended to "previous key")
	Val       cid.Cid  `cborgen:"v"` // CID pointer at this path/key
	Tree      *cid.Cid `cborgen:"t"` // [optional] pointer to lower-level subtree to the "right" of this path/key entry
}

type NodeData struct {
	Left    *cid.Cid    `cborgen:"l"` // [optional] pointer to lower-level subtree to the "left" of this path/key
	Entries []TreeEntry `cborgen:"e"` // ordered list of entries at this node
}

func (mst *MerkleSearchTree) Add(ctx context.Context, key string, val cid.Cid, knownZeros int) (*MerkleSearchTree, error) {
	keyZeros := knownZeros // is this really just to avoid rerunning the leading zeros hash?
	if keyZeros < 0 {
		keyZeros = leadingZerosOnHash(key)
	}

	layer, err := mst.getLayer(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting layer failed: %w", err)
	}

	newLeaf := NodeEntry{
		Kind: EntryLeaf,
		Key:  key,
		Val:  val,
	}

	if keyZeros == layer {
		// it belongs to me
		index, err := mst.findGtOrEqualLeafIndex(ctx, key)
		if err != nil {
			return nil, err
		}

		found, err := mst.atIndex(index)
		if err != nil {
			return nil, err
		}

		if found.isLeaf() && found.Key == key {
			return nil, fmt.Errorf("value already set at key: %s", key)
		}

		prevNode, err := mst.atIndex(index - 1)
		if err != nil {
			return nil, err
		}

		if prevNode.isUndefined() || prevNode.isLeaf() {
			return mst.spliceIn(ctx, newLeaf, index)
		}

		left, right, err := prevNode.Tree.splitAround(ctx, key)
		if err != nil {
			return nil, err
		}

		return mst.replaceWithSplit(ctx, index-1, left, newLeaf, right)
	} else if keyZeros < layer {
		index, err := mst.findGtOrEqualLeafIndex(ctx, key)
		if err != nil {
			return nil, err
		}

		prevNode, err := mst.atIndex(index - 1)
		if err != nil {
			return nil, err
		}

		if !prevNode.isUndefined() && prevNode.isTree() {
			newSubtree, err := prevNode.Tree.Add(ctx, key, val, keyZeros)
			if err != nil {
				return nil, err
			}

			return mst.updateEntry(ctx, index-1, treeEntry(newSubtree))
		} else {
			subTree, err := mst.createChild(ctx)
			if err != nil {
				return nil, err
			}

			newSubTree, err := subTree.Add(ctx, key, val, keyZeros)
			if err != nil {
				return nil, fmt.Errorf("subtree add: %w", err)
			}

			return mst.spliceIn(ctx, treeEntry(newSubTree), index)
		}
	} else {
		left, right, err := mst.splitAround(ctx, key)
		if err != nil {
			return nil, err
		}

		layer, err := mst.getLayer(ctx)
		if err != nil {
			return nil, fmt.Errorf("get layer in split case failed: %w", err)
		}

		extraLayersToAdd := keyZeros - layer

		for i := 1; i < extraLayersToAdd; i++ {
			// seems bad if both left and right are non nil
			if left != nil {
				par, err := left.createParent(ctx)
				if err != nil {
					return nil, fmt.Errorf("create left parent: %w", err)
				}
				left = par
			}

			if right != nil {
				par, err := right.createParent(ctx)
				if err != nil {
					return nil, fmt.Errorf("create right parent: %w", err)
				}
				right = par
			}

		}

		var updated []NodeEntry
		if left != nil {
			updated = append(updated, treeEntry(left))
		}

		updated = append(updated, NodeEntry{
			Kind: EntryLeaf,
			Key:  key,
			Val:  val,
		})

		if right != nil {
			updated = append(updated, treeEntry(right))
		}

		checkTreeInvariant(updated)
		newRoot, err := create(ctx, mst.cst, updated, keyZeros)
		if err != nil {
			return nil, fmt.Errorf("creating new tree after split: %w", err)
		}

		// why invalidate?
		newRoot.validPtr = false

		return newRoot, nil
	}
}

func (mst *MerkleSearchTree) Delete(ctx context.Context, k string) (*MerkleSearchTree, error) {
	ix, err := mst.findGtOrEqualLeafIndex(ctx, k)
	if err != nil {
		return nil, err
	}

	ne, err := mst.atIndex(ix)
	if err != nil {
		return nil, err
	}

	entries, err := mst.getEntries(ctx)
	if err != nil {
		return nil, err
	}

	if ne.isLeaf() && ne.Key == k {
		prev, err := mst.atIndex(ix - 1)
		if err != nil {
			return nil, err
		}

		next, err := mst.atIndex(ix + 1)
		if err != nil {
			return nil, err
		}

		if prev.isTree() && next.isTree() {
			merged, err := prev.Tree.appendMerge(ctx, next.Tree)
			if err != nil {
				return nil, err
			}
			return mst.newTree(append(append(entries[:ix-1], treeEntry(merged)), entries[ix+1:]...)), nil
		} else {
			return mst.removeEntry(ctx, ix)
		}
	}

	prev, err := mst.atIndex(ix - 1)
	if err != nil {
		return nil, err
	}

	if prev.isTree() {
		subtree, err := prev.Tree.Delete(ctx, k)
		if err != nil {
			return nil, err
		}

		subtreeEntries, err := subtree.getEntries(ctx)
		if err != nil {
			return nil, err
		}

		if len(subtreeEntries) == 0 {
			return mst.removeEntry(ctx, ix-1)
		} else {
			return mst.updateEntry(ctx, ix-1, treeEntry(subtree))
		}
	} else {
		return nil, fmt.Errorf("could not find record with key: %s", k)
	}
}

func (mst *MerkleSearchTree) appendMerge(ctx context.Context, omst *MerkleSearchTree) (*MerkleSearchTree, error) {
	mylayer, err := mst.getLayer(ctx)
	if err != nil {
		return nil, err
	}

	olayer, err := omst.getLayer(ctx)
	if err != nil {
		return nil, err
	}

	if mylayer != olayer {
		return nil, fmt.Errorf("trying to merge two nodes from different layers")
	}

	entries, err := mst.getEntries(ctx)
	if err != nil {
		return nil, err
	}

	tomergeEnts, err := omst.getEntries(ctx)
	if err != nil {
		return nil, err
	}

	lastInLeft := entries[len(entries)-1]
	firstInRight := entries[0]

	if lastInLeft.isTree() && firstInRight.isTree() {
		merged, err := lastInLeft.Tree.appendMerge(ctx, firstInRight.Tree)
		if err != nil {
			return nil, err
		}

		return mst.newTree(append(append(entries[:len(entries)-1], treeEntry(merged)), tomergeEnts[1:]...)), nil
	} else {
		return mst.newTree(append(entries, tomergeEnts...)), nil
	}
}

var ErrNotFound = fmt.Errorf("mst: not found")

func (mst *MerkleSearchTree) Get(ctx context.Context, k string) (cid.Cid, error) {
	index, err := mst.findGtOrEqualLeafIndex(ctx, k)
	if err != nil {
		return cid.Undef, err
	}

	found, err := mst.atIndex(index)
	if err != nil {
		return cid.Undef, err
	}

	if !found.isUndefined() && found.isLeaf() && found.Key == k {
		return found.Val, nil
	}

	prev, err := mst.atIndex(index - 1)
	if err != nil {
		return cid.Undef, err
	}

	if !prev.isUndefined() && prev.isTree() {
		return prev.Tree.Get(ctx, k)
	}

	return cid.Undef, ErrNotFound
}

func (mst *MerkleSearchTree) createParent(ctx context.Context) (*MerkleSearchTree, error) {
	layer, err := mst.getLayer(ctx)
	if err != nil {
		return nil, err
	}

	return NewMST(mst.cst, cid.Undef, []NodeEntry{treeEntry(mst)}, layer+1), nil
}

func (mst *MerkleSearchTree) createChild(ctx context.Context) (*MerkleSearchTree, error) {
	layer, err := mst.getLayer(ctx)
	if err != nil {
		return nil, err
	}

	return NewMST(mst.cst, cid.Undef, []NodeEntry{}, layer-1), nil
}

func (mst *MerkleSearchTree) updateEntry(ctx context.Context, ix int, entry NodeEntry) (*MerkleSearchTree, error) {
	entries, err := mst.getEntries(ctx)
	if err != nil {
		return nil, err
	}

	nents := make([]NodeEntry, len(entries))
	copy(nents, entries[:ix])
	nents[ix] = entry
	copy(nents[ix+1:], entries[ix+1:])

	checkTreeInvariant(nents)

	return mst.newTree(nents), nil
}

func (mst *MerkleSearchTree) replaceWithSplit(ctx context.Context, ix int, left *MerkleSearchTree, nl NodeEntry, right *MerkleSearchTree) (*MerkleSearchTree, error) {
	entries, err := mst.getEntries(ctx)
	if err != nil {
		return nil, err
	}
	checkTreeInvariant(entries)
	var update []NodeEntry
	update = append(update, entries[:ix]...)

	if left != nil {
		update = append(update, NodeEntry{
			Kind: EntryTree,
			Tree: left,
		})
	}

	update = append(update, nl)

	if right != nil {
		update = append(update, NodeEntry{
			Kind: EntryTree,
			Tree: right,
		})
	}

	update = append(update, entries[ix+1:]...)

	checkTreeInvariant(update)
	return mst.newTree(update), nil
}

func checkTreeInvariant(ents []NodeEntry) {
	for i := 0; i < len(ents)-1; i++ {
		if ents[i].isTree() && ents[i+1].isTree() {
			panic(fmt.Sprintf("two trees next to each other! %d %d", i, i+1))
		}
	}
}

func (mst *MerkleSearchTree) splitAround(ctx context.Context, key string) (*MerkleSearchTree, *MerkleSearchTree, error) {
	index, err := mst.findGtOrEqualLeafIndex(ctx, key)
	if err != nil {
		return nil, nil, err
	}

	entries, err := mst.getEntries(ctx)
	if err != nil {
		return nil, nil, err
	}

	leftData := entries[:index]
	rightData := entries[index:]
	left := mst.newTree(leftData)
	right := mst.newTree(rightData)

	if len(leftData) > 0 && leftData[len(leftData)-1].isTree() {
		lastInLeft := leftData[len(leftData)-1]

		nleft, err := left.removeEntry(ctx, len(leftData)-1)
		if err != nil {
			return nil, nil, err
		}
		left = nleft

		subl, subr, err := lastInLeft.Tree.splitAround(ctx, key)
		if err != nil {
			return nil, nil, err
		}

		if subl != nil {
			left, err = left.append(ctx, treeEntry(subl))
			if err != nil {
				return nil, nil, err
			}
		}

		if subr != nil {
			right, err = right.prepend(ctx, treeEntry(subr))
			if err != nil {
				return nil, nil, err
			}
		}
	}

	if left.entryCount() == 0 {
		left = nil
	}
	if right.entryCount() == 0 {
		right = nil
	}

	return left, right, nil
}

func (mst *MerkleSearchTree) entryCount() int {
	entries, err := mst.getEntries(context.TODO())
	if err != nil {
		panic(err)
	}

	return len(entries)
}

func (mst *MerkleSearchTree) append(ctx context.Context, ent NodeEntry) (*MerkleSearchTree, error) {
	entries, err := mst.getEntries(ctx)
	if err != nil {
		return nil, err
	}

	nents := make([]NodeEntry, len(entries)+1)
	copy(nents, entries)
	nents[len(nents)-1] = ent

	checkTreeInvariant(nents)
	return mst.newTree(nents), nil
}

func (mst *MerkleSearchTree) prepend(ctx context.Context, ent NodeEntry) (*MerkleSearchTree, error) {
	entries, err := mst.getEntries(ctx)
	if err != nil {
		return nil, err
	}

	nents := make([]NodeEntry, len(entries)+1)
	copy(nents[1:], entries)
	nents[0] = ent

	checkTreeInvariant(nents)
	return mst.newTree(nents), nil
}

func (mst *MerkleSearchTree) removeEntry(ctx context.Context, ix int) (*MerkleSearchTree, error) {
	entries, err := mst.getEntries(ctx)
	if err != nil {
		return nil, err
	}

	nents := make([]NodeEntry, len(entries)-1)
	copy(nents, entries[:ix])
	copy(nents[ix:], entries[ix+1:])
	checkTreeInvariant(nents)
	return mst.newTree(nents), nil
}

func (mst *MerkleSearchTree) spliceIn(ctx context.Context, entry NodeEntry, ix int) (*MerkleSearchTree, error) {
	entries, err := mst.getEntries(ctx)
	if err != nil {
		return nil, err
	}

	nents := make([]NodeEntry, len(entries)+1)
	copy(nents, entries[:ix])
	nents[ix] = entry
	copy(nents[ix+1:], entries[ix:])

	checkTreeInvariant(nents)
	return mst.newTree(nents), nil
}

func (mst *MerkleSearchTree) atIndex(ix int) (NodeEntry, error) {
	entries, err := mst.getEntries(context.TODO())
	if err != nil {
		return NodeEntry{}, err
	}

	if ix < 0 || ix >= len(entries) {
		return NodeEntry{}, nil
	}

	return entries[ix], nil
}

// this smells inefficient
func (mst *MerkleSearchTree) findGtOrEqualLeafIndex(ctx context.Context, key string) (int, error) {
	entries, err := mst.getEntries(ctx)
	if err != nil {
		return -1, err
	}

	for i, e := range entries {
		//if e.isLeaf() && bytes.Compare(e.Key, key) > 0 {
		if e.isLeaf() && e.Key >= key {
			return i, nil
		}
	}

	return len(entries), nil
}

func (mst *MerkleSearchTree) getEntries(ctx context.Context) ([]NodeEntry, error) {
	if mst.entries != nil {
		return mst.entries, nil
	}

	if mst.pointer != cid.Undef {
		var nd NodeData
		if err := mst.cst.Get(ctx, mst.pointer, &nd); err != nil {
			return nil, err
		}

		entries, err := entriesFromNodeData(ctx, &nd, mst.cst)
		if err != nil {
			return nil, err
		}
		if entries == nil {
			panic("got nil entries from node data decoding")
		}
		mst.entries = entries
		return entries, nil
	}

	return nil, fmt.Errorf("no entries or cid provided")
}

func entriesFromNodeData(ctx context.Context, nd *NodeData, cst cbor.IpldStore) ([]NodeEntry, error) {
	layer := -1
	if len(nd.Entries) > 0 {
		firstLeaf := nd.Entries[0]
		layer = leadingZerosOnHash(firstLeaf.KeySuffix)
	}

	entries, err := deserializeNodeData(ctx, cst, nd, layer)
	if err != nil {
		return nil, err
	}

	return entries, nil
}

func (mst *MerkleSearchTree) getPointer(ctx context.Context) (cid.Cid, error) {
	if mst.validPtr {
		return mst.pointer, nil
	}

	// XXX(bnewbold): refactor to be more idiomatic golang
	entries, err := mst.getEntries(ctx)
	if err != nil {
		return cid.Undef, err
	}

	var outdated []*MerkleSearchTree
	for _, e := range entries {
		if e.Kind == EntryTree && !e.Tree.validPtr {
			outdated = append(outdated, e.Tree)
		}
	}

	if len(outdated) > 0 {
		// this block feels... off
		for _, o := range outdated {
			_, err := o.getPointer(ctx)
			if err != nil {
				return cid.Undef, err
			}
		}
		ne, err := mst.getEntries(ctx)
		if err != nil {
			return cid.Undef, err
		}
		entries = ne
	}

	ptr, err := cidForEntries(ctx, entries, mst.cst)
	if err != nil {
		return cid.Undef, err
	}

	mst.pointer = ptr
	mst.validPtr = true

	return mst.pointer, nil
}

func cidForEntries(ctx context.Context, entries []NodeEntry, cst cbor.IpldStore) (cid.Cid, error) {
	nd, err := serializeNodeData(entries)
	if err != nil {
		return cid.Undef, fmt.Errorf("serializing new entries: %w", err)
	}

	return cst.Put(ctx, nd)
}

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
	if a < b {
		return a
	}
	return b
}

// how many leading chars are identical between the two strings?
func countPrefixLen(a, b string) int {
	count := min(len(a), len(b))
	for i := 0; i < count; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return count
}

func deserializeNodeData(ctx context.Context, cst cbor.IpldStore, nd *NodeData, layer int) ([]NodeEntry, error) {
	entries := []NodeEntry{}
	if nd.Left != nil {
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

func (mst *MerkleSearchTree) getLayer(ctx context.Context) (int, error) {
	if mst.layer >= 0 {
		return mst.layer, nil
	}

	entries, err := mst.getEntries(ctx)
	if err != nil {
		return -1, err
	}

	mst.layer = layerForEntries(entries)
	if mst.layer < 0 {
		// still empty!
		mst.layer = 0
	}

	return mst.layer, nil
}

func log2(v int) int {
	var out int
	for v > 1 {
		out++
		v = v / 2
	}
	return out
}

// Used to determine the "depth" of keys in an MST.
// For atproto, the "fanout" is always 16, so we count "zeros" in chunks of
// 4-bits. Eg, a leading 0x00 byte is 2 "zeros".
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

func (mst *MerkleSearchTree) WalkLeavesFrom(ctx context.Context, key string, cb func(n NodeEntry) error) error {
	index, err := mst.findGtOrEqualLeafIndex(ctx, key)
	if err != nil {
		return err
	}

	entries, err := mst.getEntries(ctx)
	if err != nil {
		return err
	}

	if index > 0 {
		prev := entries[index-1]
		if !prev.isUndefined() && prev.isTree() {
			if err := prev.Tree.WalkLeavesFrom(ctx, key, cb); err != nil {
				return err
			}
		}
	}

	for _, e := range entries[index:] {
		if e.isLeaf() {
			if err := cb(e); err != nil {
				return err
			}
		} else {
			if err := e.Tree.WalkLeavesFrom(ctx, key, cb); err != nil {
				return err
			}
		}
	}
	return nil
}
