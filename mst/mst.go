// Package mst contains a Merkle Search Tree (MST) implementation for atproto.
//
// This implementation is a port of the Typescript implementation in the
// `atproto` git repo.
//
// ## Notes copied from TS repo
//
// This is an implementation of a Merkle Search Tree (MST)
// The data structure is described here: https://hal.inria.fr/hal-02303490/document
// The MST is an ordered, insert-order-independent, deterministic tree.
// Keys are laid out in alphabetic order.
// The key insight of an MST is that each key is hashed and starting 0s are counted
// to determine which layer it falls on (5 zeros for ~32 fanout).
// This is a merkle tree, so each subtree is referred to by it's hash (CID).
// When a leaf is changed, ever tree on the path to that leaf is changed as well,
// thereby updating the root hash.
//
// For atproto, we use SHA-256 as the key hashing algorithm, and ~16 fanout
// (4-bits of zero per layer).
//
// NOTE: currently keys are strings, not bytes. Because UTF-8 strings can't be
// safely split at arbitrary byte boundaries (the results are not necessarily
// valid UTF-8 strings), this means that "wide" characters not really supported
// in keys, particularly across programming language implementations. We
// recommend sticking with simple alphanumeric (ASCII) strings.
//
// A couple notes on CBOR encoding:
//
// There are never two neighboring subtrees.
// Therefore, we can represent a node as an array of
// leaves & pointers to their right neighbor (possibly null),
// along with a pointer to the left-most subtree (also possibly null).
//
// Most keys in a subtree will have overlap.
// We do compression on prefixes by describing keys as:
// - the length of the prefix that it shares in common with the preceding key
// - the rest of the string
//
// For example:
// If the first leaf in a tree is `bsky/posts/abcdefg` and the second is `bsky/posts/abcdehi`
// Then the first will be described as `prefix: 0, key: 'bsky/posts/abcdefg'`,
// and the second will be described as `prefix: 16, key: 'hi'.`
package mst

import (
	"context"
	"fmt"
	"reflect"

	"github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
)

// nodeKind is the type of node in the MST.
type nodeKind uint8

const (
	entryUndefined nodeKind = 0
	entryLeaf      nodeKind = 1
	entryTree      nodeKind = 2
)

// nodeEntry is a node in the MST.
//
// Following the Typescript implementation, this is basically a flexible
// "TreeEntry" (aka "leaf") which might also be the "Left" pointer on a
// NodeData (aka "tree"). This type flexibility is not idiomatic in Go, but
// we are keeping this a very direct port.
type nodeEntry struct {
	Kind nodeKind
	Key  string
	Val  cid.Cid
	Tree *MerkleSearchTree
}

func mkTreeEntry(t *MerkleSearchTree) nodeEntry {
	return nodeEntry{
		Kind: entryTree,
		Tree: t,
	}
}

func (ne nodeEntry) isTree() bool {
	return ne.Kind == entryTree
}

func (ne nodeEntry) isLeaf() bool {
	return ne.Kind == entryLeaf
}

func (ne nodeEntry) isUndefined() bool {
	return ne.Kind == entryUndefined
}

// golang-specific helper to sanity check nodeEntry semantics
func checkTreeInvariant(ents []nodeEntry) {
	for i := 0; i < len(ents)-1; i++ {
		if ents[i].isTree() && ents[i+1].isTree() {
			panic(fmt.Sprintf("two trees next to each other! %d %d", i, i+1))
		}
	}
}

// CBORTypes returns the types in this package that need to be registered with
// the CBOR codec.
func CBORTypes() []reflect.Type {
	return []reflect.Type{
		reflect.TypeOf(NodeData{}),
		reflect.TypeOf(TreeEntry{}),
	}
}

// MST tree node as gets serialized to CBOR. Note that the CBOR fields are all
// single-character.
type NodeData struct {
	Left    *cid.Cid    `cborgen:"l"` // [nullable] pointer to lower-level subtree to the "left" of this path/key
	Entries []TreeEntry `cborgen:"e"` // ordered list of entries at this node
}

// TreeEntry are elements of NodeData's Entries.
type TreeEntry struct {
	PrefixLen int64    `cborgen:"p"` // count of characters shared with previous path/key in tree
	KeySuffix []byte   `cborgen:"k"` // remaining part of path/key (appended to "previous key")
	Val       cid.Cid  `cborgen:"v"` // CID pointer at this path/key
	Tree      *cid.Cid `cborgen:"t"` // [nullable] pointer to lower-level subtree to the "right" of this path/key entry
}

// MerkleSearchTree represents an MST tree node (NodeData type). It can be in
// several levels of hydration: fully hydrated (entries and "pointer" (CID)
// computed); dirty (entries correct, but pointer (CID) not valid); virtual
// (pointer is defined, no entries have been pulled from blockstore)
//
// MerkleSearchTree values are immutable. Methods return copies with changes.
type MerkleSearchTree struct {
	cst      cbor.IpldStore
	entries  []nodeEntry // non-nil when "hydrated"
	layer    int
	pointer  cid.Cid
	validPtr bool
}

// NewEmptyMST reports a new empty MST using cst as its storage.
func NewEmptyMST(cst cbor.IpldStore) *MerkleSearchTree {
	return createMST(cst, cid.Undef, []nodeEntry{}, 0)
}

// Typescript: MST.create(storage, entries, layer, fanout) -> MST
func createMST(cst cbor.IpldStore, ptr cid.Cid, entries []nodeEntry, layer int) *MerkleSearchTree {
	mst := &MerkleSearchTree{
		cst:      cst,
		pointer:  ptr,
		layer:    layer,
		entries:  entries,
		validPtr: ptr.Defined(),
	}

	return mst
}

// TODO: Typescript: MST.fromData(storage, data, layer=null, fanout)

// This is poorly named in both implementations, because it is lazy
// Typescript: MST.load(storage, cid, layer=null, fanout) -> MST
func LoadMST(cst cbor.IpldStore, root cid.Cid) *MerkleSearchTree {
	return createMST(cst, root, nil, -1)
}

// === "Immutability" ===

// "We never mutate an MST, we just return a new MST with updated values"
// Typescript: MST.newTree(entries) -> MST
func (mst *MerkleSearchTree) newTree(entries []nodeEntry) *MerkleSearchTree {
	if entries == nil {
		panic("nil entries passed to newTree")
	}
	return createMST(mst.cst, cid.Undef, entries, mst.layer)
}

// === "Getters (lazy load)" ===

// "We don't want to load entries of every subtree, just the ones we need"
// Typescript: MST.getEntries() -> nodeEntry[]
func (mst *MerkleSearchTree) getEntries(ctx context.Context) ([]nodeEntry, error) {
	// if we are "hydrated", entries are available
	if mst.entries != nil {
		return mst.entries, nil
	}

	// otherwise this is a virtual/pointer struct and we need to hydrate from
	// blockstore before returning entries
	if mst.pointer != cid.Undef {
		var nd NodeData
		if err := mst.cst.Get(ctx, mst.pointer, &nd); err != nil {
			return nil, err
		}
		// NOTE(bnewbold): Typescript version computes layer in-place here, but
		// the entriesFromNodeData() helper does that for us in golang
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

	return nil, fmt.Errorf("no entries or self-pointer (CID) on MerkleSearchTree")
}

// golang-specific helper that calls in to deserializeNodeData
func entriesFromNodeData(ctx context.Context, nd *NodeData, cst cbor.IpldStore) ([]nodeEntry, error) {
	layer := -1
	if len(nd.Entries) > 0 {
		// NOTE(bnewbold): can compute the layer on the first KeySuffix, because for the first entry that field is a complete key
		firstLeaf := nd.Entries[0]
		layer = leadingZerosOnHashBytes(firstLeaf.KeySuffix)
	}

	entries, err := deserializeNodeData(ctx, cst, nd, layer)
	if err != nil {
		return nil, err
	}

	return entries, nil
}

// "We don't hash the node on every mutation for performance reasons. Instead we keep track of whether the pointer is outdated and only (recursively) calculate when needed"
// Typescript: MST.getPointer() -> CID
func (mst *MerkleSearchTree) GetPointer(ctx context.Context) (cid.Cid, error) {
	if mst.validPtr {
		return mst.pointer, nil
	}

	// NOTE(bnewbold): this is a bit different from how Typescript version works
	// update in-place; first ensure that mst.entries is hydrated
	_, err := mst.getEntries(ctx)
	if err != nil {
		return cid.Undef, err
	}

	for i, e := range mst.entries {
		if e.isTree() {
			if !e.Tree.validPtr {
				_, err := e.Tree.GetPointer(ctx)
				if err != nil {
					return cid.Undef, err
				}
				mst.entries[i] = e
			}
		}
	}

	nptr, err := cidForEntries(ctx, mst.entries, mst.cst)
	if err != nil {
		return cid.Undef, err
	}
	mst.pointer = nptr
	mst.validPtr = true

	return mst.pointer, nil
}

// "In most cases, we get the layer of a node from a hint on creation"
// "In the case of the topmost node in the tree, we look for a key in the node & determine the layer"
// "In the case where we don't find one, we recurse down until we do."
// "If we still can't find one, then we have an empty tree and the node is layer 0"
// Typescript: MST.getLayer() -> number
func (mst *MerkleSearchTree) getLayer(ctx context.Context) (int, error) {
	layer, err := mst.attemptGetLayer(ctx)
	if err != nil {
		return -1, err
	}
	if layer < 0 {
		mst.layer = 0
	} else {
		mst.layer = layer
	}
	return mst.layer, nil
}

// Typescript: MST.attemptGetLayer() -> number
func (mst *MerkleSearchTree) attemptGetLayer(ctx context.Context) (int, error) {
	if mst.layer >= 0 {
		return mst.layer, nil
	}

	entries, err := mst.getEntries(ctx)
	if err != nil {
		return -1, err
	}

	layer := layerForEntries(entries)
	if layer < 0 {
		// NOTE(bnewbold): updated this from typescript
		for _, e := range entries {
			if e.isTree() {
				childLayer, err := e.Tree.attemptGetLayer(ctx)
				if err != nil {
					return -1, err
				}
				if childLayer >= 0 {
					layer = childLayer + 1
					break
				}
			}
		}
	}

	if layer >= 0 {
		mst.layer = layer
	}
	return mst.layer, nil
}

// === "Core functionality" ===

// NOTE: MST.getUnstoredBlocks() not needed; we are always working out of
// blockstore in this implementation

// "Adds a new leaf for the given key/value pair. Throws if a leaf with that key already exists"
// Typescript: MST.add(key, value, knownZeros?) -> MST
func (mst *MerkleSearchTree) Add(ctx context.Context, key string, val cid.Cid, knownZeros int) (*MerkleSearchTree, error) {

	// NOTE(bnewbold): this is inefficient (recurses), but matches TS implementation
	err := ensureValidMstKey(key)
	if err != nil {
		return nil, err
	}

	if val == cid.Undef {
		return nil, fmt.Errorf("tried to insert an undef CID")
	}

	keyZeros := knownZeros
	if keyZeros < 0 {
		keyZeros = leadingZerosOnHash(key)
	}

	layer, err := mst.getLayer(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting layer failed: %w", err)
	}

	newLeaf := nodeEntry{
		Kind: entryLeaf,
		Key:  key,
		Val:  val,
	}

	if keyZeros == layer {
		// it belongs in this layer
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
			// "if entry before is a leaf, (or we're on far left) we can just splice in"
			return mst.spliceIn(ctx, newLeaf, index)
		}

		// "else we try to split the subtree around the key"
		left, right, err := prevNode.Tree.splitAround(ctx, key)
		if err != nil {
			return nil, err
		}
		// NOTE(bnewbold): added this replaceWithSplit() call
		return mst.replaceWithSplit(ctx, index-1, left, newLeaf, right)

	} else if keyZeros < layer {
		// "it belongs on a lower layer"
		index, err := mst.findGtOrEqualLeafIndex(ctx, key)
		if err != nil {
			return nil, err
		}

		prevNode, err := mst.atIndex(index - 1)
		if err != nil {
			return nil, err
		}

		if !prevNode.isUndefined() && prevNode.isTree() {
			// "if entry before is a tree, we add it to that tree"
			newSubtree, err := prevNode.Tree.Add(ctx, key, val, keyZeros)
			if err != nil {
				return nil, err
			}

			return mst.updateEntry(ctx, index-1, mkTreeEntry(newSubtree))
		} else {
			subTree, err := mst.createChild(ctx)
			if err != nil {
				return nil, err
			}

			newSubTree, err := subTree.Add(ctx, key, val, keyZeros)
			if err != nil {
				return nil, fmt.Errorf("subtree add: %w", err)
			}

			return mst.spliceIn(ctx, mkTreeEntry(newSubTree), index)
		}

	} else {
		// "it belongs on a higher layer & we must push the rest of the tree down"
		left, right, err := mst.splitAround(ctx, key)
		if err != nil {
			return nil, err
		}

		// "if the newly added key has >=2 more leading zeros than the current highest layer then we need to add in structural nodes in between as well"
		layer, err := mst.getLayer(ctx)
		if err != nil {
			return nil, fmt.Errorf("get layer in split case failed: %w", err)
		}

		extraLayersToAdd := keyZeros - layer

		// "intentionally starting at 1, since first layer is taken care of by split"
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

		var updated []nodeEntry
		if left != nil {
			updated = append(updated, mkTreeEntry(left))
		}

		updated = append(updated, nodeEntry{
			Kind: entryLeaf,
			Key:  key,
			Val:  val,
		})

		if right != nil {
			updated = append(updated, mkTreeEntry(right))
		}

		checkTreeInvariant(updated)
		newRoot := createMST(mst.cst, cid.Undef, updated, keyZeros)

		// NOTE(bnewbold): We do want to invalid the CID (because this node has
		// changed, and we are "lazy" about recomputing). Setting this flag
		// is redundant with passing cid.Undef to NewMST just above, but
		// keeping because it is explicit and matches the explicit invalidation
		// that happens in the Typescript code
		newRoot.validPtr = false

		return newRoot, nil
	}
}

var ErrNotFound = fmt.Errorf("mst: not found")

// "Gets the value at the given key"
// Typescript: MST.get(key) -> (CID|null)
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

// "Edits the value at the given key. Throws if the given key does not exist"
// Typescript: MST.update(key, value) -> MST
func (mst *MerkleSearchTree) Update(ctx context.Context, k string, val cid.Cid) (*MerkleSearchTree, error) {

	// NOTE(bnewbold): this is inefficient (recurses), but matches TS implementation
	err := ensureValidMstKey(k)
	if err != nil {
		return nil, err
	}

	if val == cid.Undef {
		return nil, fmt.Errorf("tried to insert an undef CID")
	}

	index, err := mst.findGtOrEqualLeafIndex(ctx, k)
	if err != nil {
		return nil, err
	}

	found, err := mst.atIndex(index)
	if err != nil {
		return nil, err
	}

	if !found.isUndefined() && found.isLeaf() && found.Key == k {
		// NOTE(bnewbold): updated here
		return mst.updateEntry(ctx, index, nodeEntry{
			Kind: entryLeaf,
			Key:  string(k),
			Val:  val,
		})
	}

	prev, err := mst.atIndex(index - 1)
	if err != nil {
		return nil, err
	}

	if !prev.isUndefined() && prev.isTree() {
		updatedTree, err := prev.Tree.Update(ctx, k, val)
		if err != nil {
			return nil, err
		}

		return mst.updateEntry(ctx, index-1, mkTreeEntry(updatedTree))
	}

	return nil, ErrNotFound
}

// "Deletes the value at the given key"
// Typescript: MST.delete(key) -> MST
func (mst *MerkleSearchTree) Delete(ctx context.Context, k string) (*MerkleSearchTree, error) {
	altered, err := mst.deleteRecurse(ctx, k)
	if err != nil {
		return nil, err
	}
	return altered.trimTop(ctx)
}

// Typescript: MST.deleteRecurse(key) -> MST
func (mst *MerkleSearchTree) deleteRecurse(ctx context.Context, k string) (*MerkleSearchTree, error) {
	ix, err := mst.findGtOrEqualLeafIndex(ctx, k)
	if err != nil {
		return nil, err
	}

	found, err := mst.atIndex(ix)
	if err != nil {
		return nil, err
	}

	// "if found, remove it on this level"
	if found.isLeaf() && found.Key == k {
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
			entries, err := mst.getEntries(ctx)
			if err != nil {
				return nil, err
			}
			return mst.newTree(append(append(entries[:ix-1], mkTreeEntry(merged)), entries[ix+2:]...)), nil
		} else {
			return mst.removeEntry(ctx, ix)
		}
	}

	// "else recurse down to find it"
	prev, err := mst.atIndex(ix - 1)
	if err != nil {
		return nil, err
	}

	if prev.isTree() {
		subtree, err := prev.Tree.deleteRecurse(ctx, k)
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
			return mst.updateEntry(ctx, ix-1, mkTreeEntry(subtree))
		}
	} else {
		return nil, fmt.Errorf("could not find record with key: %s", k)
	}
}

// === "Simple Operations" ===

// "update entry in place"
// Typescript: MST.updateEntry(index, entry) -> MST
func (mst *MerkleSearchTree) updateEntry(ctx context.Context, ix int, entry nodeEntry) (*MerkleSearchTree, error) {
	entries, err := mst.getEntries(ctx)
	if err != nil {
		return nil, err
	}

	nents := make([]nodeEntry, len(entries))
	copy(nents, entries[:ix])
	nents[ix] = entry
	copy(nents[ix+1:], entries[ix+1:])

	checkTreeInvariant(nents)
	return mst.newTree(nents), nil
}

// "remove entry at index"
func (mst *MerkleSearchTree) removeEntry(ctx context.Context, ix int) (*MerkleSearchTree, error) {
	entries, err := mst.getEntries(ctx)
	if err != nil {
		return nil, err
	}

	nents := make([]nodeEntry, len(entries)-1)
	copy(nents, entries[:ix])
	copy(nents[ix:], entries[ix+1:])

	checkTreeInvariant(nents)
	return mst.newTree(nents), nil
}

// "append entry to end of the node"
func (mst *MerkleSearchTree) append(ctx context.Context, ent nodeEntry) (*MerkleSearchTree, error) {
	entries, err := mst.getEntries(ctx)
	if err != nil {
		return nil, err
	}

	nents := make([]nodeEntry, len(entries)+1)
	copy(nents, entries)
	nents[len(nents)-1] = ent

	checkTreeInvariant(nents)
	return mst.newTree(nents), nil
}

// "prepend entry to start of the node"
func (mst *MerkleSearchTree) prepend(ctx context.Context, ent nodeEntry) (*MerkleSearchTree, error) {
	entries, err := mst.getEntries(ctx)
	if err != nil {
		return nil, err
	}

	nents := make([]nodeEntry, len(entries)+1)
	copy(nents[1:], entries)
	nents[0] = ent

	checkTreeInvariant(nents)
	return mst.newTree(nents), nil
}

// "returns entry at index"
// Apparently returns null if nothing at index, which seems brittle
func (mst *MerkleSearchTree) atIndex(ix int) (nodeEntry, error) {
	entries, err := mst.getEntries(context.TODO())
	if err != nil {
		return nodeEntry{}, err
	}

	// TODO(bnewbold): same as Typescript, but shouldn't this error instead of returning null?
	if ix < 0 || ix >= len(entries) {
		return nodeEntry{}, nil
	}

	return entries[ix], nil
}

// NOTE(bnewbold): unlike Typescript, golang does not really need the slice(start?, end?) helper

// "inserts entry at index"
func (mst *MerkleSearchTree) spliceIn(ctx context.Context, entry nodeEntry, ix int) (*MerkleSearchTree, error) {
	entries, err := mst.getEntries(ctx)
	if err != nil {
		return nil, err
	}

	nents := make([]nodeEntry, len(entries)+1)
	copy(nents, entries[:ix])
	nents[ix] = entry
	copy(nents[ix+1:], entries[ix:])

	checkTreeInvariant(nents)
	return mst.newTree(nents), nil
}

// "replaces an entry with [ Maybe(tree), Leaf, Maybe(tree) ]"
func (mst *MerkleSearchTree) replaceWithSplit(ctx context.Context, ix int, left *MerkleSearchTree, nl nodeEntry, right *MerkleSearchTree) (*MerkleSearchTree, error) {
	entries, err := mst.getEntries(ctx)
	if err != nil {
		return nil, err
	}
	checkTreeInvariant(entries)
	var update []nodeEntry
	update = append(update, entries[:ix]...)

	if left != nil {
		update = append(update, nodeEntry{
			Kind: entryTree,
			Tree: left,
		})
	}

	update = append(update, nl)

	if right != nil {
		update = append(update, nodeEntry{
			Kind: entryTree,
			Tree: right,
		})
	}

	update = append(update, entries[ix+1:]...)

	checkTreeInvariant(update)
	return mst.newTree(update), nil
}

// "if the topmost node in the tree only points to another tree, trim the top and return the subtree"
func (mst *MerkleSearchTree) trimTop(ctx context.Context) (*MerkleSearchTree, error) {
	entries, err := mst.getEntries(ctx)
	if err != nil {
		return nil, err
	}
	if len(entries) == 1 && entries[0].isTree() {
		return entries[0].Tree.trimTop(ctx)
	} else {
		return mst, nil
	}
}

// === "Subtree & Splits" ===

// "Recursively splits a sub tree around a given key"
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

	// "if the far right of the left side is a subtree, we need to split it on the key as well"
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
			left, err = left.append(ctx, mkTreeEntry(subl))
			if err != nil {
				return nil, nil, err
			}
		}

		if subr != nil {
			right, err = right.prepend(ctx, mkTreeEntry(subr))
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

// "The simple merge case where every key in the right tree is greater than every key in the left tree (used primarily for deletes)"
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
	firstInRight := tomergeEnts[0] // NOTE(bnewbold): bug fixed here, I think

	if lastInLeft.isTree() && firstInRight.isTree() {
		merged, err := lastInLeft.Tree.appendMerge(ctx, firstInRight.Tree)
		if err != nil {
			return nil, err
		}

		return mst.newTree(append(append(entries[:len(entries)-1], mkTreeEntry(merged)), tomergeEnts[1:]...)), nil
	} else {
		return mst.newTree(append(entries, tomergeEnts...)), nil
	}
}

// === "Create relatives" ===

func (mst *MerkleSearchTree) createChild(ctx context.Context) (*MerkleSearchTree, error) {
	layer, err := mst.getLayer(ctx)
	if err != nil {
		return nil, err
	}

	return createMST(mst.cst, cid.Undef, []nodeEntry{}, layer-1), nil
}

func (mst *MerkleSearchTree) createParent(ctx context.Context) (*MerkleSearchTree, error) {
	layer, err := mst.getLayer(ctx)
	if err != nil {
		return nil, err
	}

	return createMST(mst.cst, cid.Undef, []nodeEntry{mkTreeEntry(mst)}, layer+1), nil
}

// === "Finding insertion points" ===

// NOTE(@why): this smells inefficient
// "finds index of first leaf node that is greater than or equal to the value"
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

	// "if we can't find, we're on the end"
	return len(entries), nil
}

// === "List operations (partial tree traversal)" ===

// WalkLeavesFrom walks the leaves of the tree, calling the cb callback on each
// key that's greater than or equal to the provided from key.
// If cb returns an error, the walk is aborted and the error is returned.
func (mst *MerkleSearchTree) WalkLeavesFrom(ctx context.Context, from string, cb func(key string, val cid.Cid) error) error {
	index, err := mst.findGtOrEqualLeafIndex(ctx, from)
	if err != nil {
		return err
	}

	entries, err := mst.getEntries(ctx)
	if err != nil {
		return fmt.Errorf("get entries: %w", err)
	}

	if index > 0 {
		prev := entries[index-1]
		if !prev.isUndefined() && prev.isTree() {
			if err := prev.Tree.WalkLeavesFrom(ctx, from, cb); err != nil {
				return fmt.Errorf("walk leaves %d: %w", index, err)
			}
		}
	}

	for i, e := range entries[index:] {
		if e.isLeaf() {
			if err := cb(e.Key, e.Val); err != nil {
				return err
			}
		} else {
			if err := e.Tree.WalkLeavesFrom(ctx, from, cb); err != nil {
				return fmt.Errorf("walk leaves from (%d): %w", i, err)
			}
		}
	}
	return nil
}

// TODO: Typescript: MST.list(count?, after?, before?) -> Leaf[]
// TODO: Typescript: MST.listWithPrefix(prefix, count?) -> Leaf[]

// "Walk full tree & emit nodes, consumer can bail at any point by returning false"
// TODO: Typescript: MST.walk() -> nodeEntry (iterator)
// TODO: Typescript: MST.paths() -> nodeEntry[][]
// TODO: Typescript: MST.allNodes() -> nodeEntry[]
// TODO: Typescript: MST.allCids() -> CidSet
// TODO: Typescript: MST.leaves() -> Leaf[]
// TODO: Typescript: MST.leafCount() -> number

// TODO: Typescript: MST.walkReachable() -> nodeEntry (iterator)
// TODO: Typescript: MST.reachableLeaves() -> Leaf[]

// TODO: Typescript: MST.writeToCarStream(car) -> ()
// TODO: Typescript: MST.cidsForPath(car) -> CID[]
