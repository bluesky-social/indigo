package mst

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/ipfs/go-cid"
)

func DebugPrintMap(m map[string]cid.Cid) {
	keys := make([]string, len(m))
	i := 0
	for k := range m {
		keys[i] = k
		i++
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("%s\t%s\n", k, m[k])
	}
}

func DebugPrintTree(n *Node, depth int) {
	if n == nil {
		fmt.Printf("EMPTY TREE")
		return
	}
	if depth == 0 {
		fmt.Printf("tree root (height=%d)\n", n.Height)
	}
	for i, e := range n.Entries {
		if depth > 0 && i == 0 {
			if len(n.Entries) > 1 {
				fmt.Printf("┬")
			} else {
				fmt.Printf("─")
			}
		} else {
			for range depth {
				fmt.Printf("│")
			}
			if i+1 == len(n.Entries) {
				fmt.Printf("└")
			} else {
				fmt.Printf("├")
			}
		}
		if e.IsValue() {
			fmt.Printf(" (%d) %s -> %s\n", HeightForKey(e.Key), e.Key, e.Value)
		} else if e.IsChild() {
			if e.Child != nil {
				DebugPrintTree(e.Child, depth+1)
			} else {
				fmt.Printf(" (partial) %s", e.ChildCID)
			}
		}
	}
}

func DebugTreeStructure(n *Node, height int, key []byte) error {
	if n == nil {
		return fmt.Errorf("nil tree")
	}
	if n.CID == nil && n.Dirty == false {
		return fmt.Errorf("node missing CID, but not marked dirty")
	}
	if len(n.Entries) == 0 {
		return fmt.Errorf("empty tree node")
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
				if err := DebugTreeStructure(e.Child, height-1, key); err != nil {
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
				return fmt.Errorf("wrong height for key")
			}
		} else {
			return fmt.Errorf("entry was neither child nor value")
		}
	}
	return nil
}

func DebugCountEntries(n *Node) int {
	if n == nil {
		return 0
	}
	count := 0
	for _, e := range n.Entries {
		if e.IsValue() {
			count++
		}
		if e.IsChild() && e.Child != nil {
			count += DebugCountEntries(e.Child)
		}
	}
	return count
}

func DebugPrintNodePointers(n *Node) {
	fmt.Printf("%p\n", n)
	if n == nil {
		return
	}
	for _, e := range n.Entries {
		if e.IsChild() && e.Child != nil {
			DebugPrintNodePointers(e.Child)
		}
	}
}
