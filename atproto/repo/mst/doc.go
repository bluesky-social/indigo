/*
Implementation of the Merkle Search Tree (MST) data structure for atproto.

## Terminology

node: any node in the tree. nodes can contain multiple entries. they should never be entirely "empty", unless the entire tree is a single empty node, but they might only contain "child" pointers

entry: nodes contain multiple entries. these can include both key/CID pairs, or pointers to child nodes. entries are always lexically sorted, with "child" entries pointing to nodes containing (recursively) keys in the appropriate lexical range. there should never be multiple "child" entries adjacent in a single node (they should be merged instead)

tree: an overall tree of nodes

## Tricky Bits

When inserting:

- the inserted key might be on a "higher" layer than the current top of the tree, in which case new parent tree nodes need to be created
- "parent" or "child" insertions might be multiple layers away from the starting node, with intermediate nodes created
- inserting a "value" entry in a node might require "splitting" a child node, if the key on the current layer would have fallen within the lexical range of the child

When removing:

- deleting an entry from a node might result in a "merge" of two child nodes which are no longer "split"
- removing a "value" entry from the top of the tree might make it a simple pointer down to a child. in this case the top of the tree should be "trimmed" (this might involve multiple layers of trimming)

When inverting operations:

- need additional "proof" blocks to invert deletions. basically need the proof blocks for any keys (at any layer) directly adjacent to the deleted block
- if an entry is removed from the top of a partial tree and results in "trimming", and the child node is not available, the overall tree root CID might still be known

## Hacking

Be careful with go slices. Need to avoid creating multiple references (slices) of the same underlying array, which can lead to "mutation at a distance" in ways that are hard to debug.
*/
package mst
