package mst

import (
	"bytes"
	"fmt"
)

func (t *Tree) Verify() error {
	if t.Root == nil {
		return fmt.Errorf("tree missing root node")
	}
	return t.Root.verifyStructure(-1, nil)
}

func (n *Node) verifyStructure(height int, key []byte) error {
	if n == nil {
		return fmt.Errorf("nil node")
	}
	if n.Stub {
		return fmt.Errorf("stub node")
	}
	if n.CID == nil && n.Dirty == false {
		return fmt.Errorf("node missing CID, but not marked dirty")
	}
	if len(n.Entries) == 0 {
		if height >= 0 {
			return fmt.Errorf("empty tree node")
		}
		// entire tree is empty
		return nil
	}

	if height < 0 {
		// do a quick pass to compute current height
		for _, e := range n.Entries {
			if e.IsValue() {
				height = HeightForKey(e.Key)
				break
			}
		}
	}
	if height < 0 {
		return fmt.Errorf("top of tree is just a pointer to child")
	}
	if n.Height == -1 || n.Height != height {
		return fmt.Errorf("node has incorrect height: %d", n.Height)
	}

	lastWasChild := false
	for _, e := range n.Entries {
		if e.IsChild() {
			if lastWasChild {
				return fmt.Errorf("sibling children in entries list")
			}
			lastWasChild = true
			if e.IsValue() {
				return fmt.Errorf("entry is both a child and a value")
			}
			if height == 0 {
				return fmt.Errorf("child below zero height")
			}
			if e.Child != nil {
				if err := e.Child.verifyStructure(height-1, key); err != nil {
					return err
				}
			}
		} else if e.IsValue() {
			lastWasChild = false
			if bytes.Equal(key, e.Key) {
				return fmt.Errorf("duplicate key in tree")
			}
			if bytes.Compare(key, e.Key) > 0 {
				return fmt.Errorf("out of order keys")
			}
			key = e.Key
			if height != HeightForKey(e.Key) {
				return fmt.Errorf("wrong height for key: %d", HeightForKey(e.Key))
			}
		} else {
			return fmt.Errorf("entry was neither child nor value")
		}
	}
	return nil
}
