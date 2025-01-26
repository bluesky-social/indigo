package mst

import (
	"fmt"

	"github.com/ipfs/go-cid"
)

// Reads the value (CID) corresponding to the key. If key is not in the tree, returns (nil, nil).
//
// n: Node at top of sub-tree to operate on. Must not be nil.
// key: key or path being inserted. must not be empty/nil
// height: tree height corresponding to key. if a negative value is provided, will be computed; use -1 instead of 0 if height is not known
func nodeGet(n *Node, key []byte, height int) (*cid.Cid, error) {
	if height < 0 {
		height = HeightForKey(key)
	}

	if height > n.Height {
		// key from a higher layer; key was not in tree
		return nil, nil
	}

	if height < n.Height {
		// look for a child node
		idx := findExistingChild(n, key)
		if idx >= 0 {
			if n.Entries[idx].Child == nil {
				return nil, fmt.Errorf("could not search for key: %w", ErrPartialTree)
			}
			return nodeGet(n.Entries[idx].Child, key, height)
		}
		// otherwise, not found
		return nil, nil
	}

	// search at this height
	idx := findExistingEntry(n, key)
	if idx >= 0 {
		return n.Entries[idx].Value, nil
	}

	// not found
	return nil, nil
}
