package mst

import (
	"fmt"
	"sort"

	"github.com/ipfs/go-cid"
)

func debugPrintMap(m map[string]cid.Cid) {
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

// This function is not very well implemented or correct. Should probably switch to Devin's `goat repo mst` code.
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
				fmt.Printf("─ (%d; partial) %s\n", n.Height-1, e.ChildCID)
			}
		} else {
			fmt.Printf(" BAD NODE\n")
		}
	}
}

func debugCountEntries(n *Node) int {
	if n == nil {
		return 0
	}
	count := 0
	for _, e := range n.Entries {
		if e.IsValue() {
			count++
		}
		if e.IsChild() && e.Child != nil {
			count += debugCountEntries(e.Child)
		}
	}
	return count
}

func debugPrintNodePointers(n *Node) {
	if n == nil {
		return
	}
	fmt.Printf("%p %p\n", n, n.Entries)
	for _, e := range n.Entries {
		if e.IsChild() && e.Child != nil {
			debugPrintNodePointers(e.Child)
		}
	}
}

func debugPrintChildPointers(n *Node) {
	if n == nil {
		return
	}
	for _, e := range n.Entries {
		if e.IsChild() && e.Child != nil {
			fmt.Printf("CHILD PTR: %p entry: %p\n", e.Child, &e)
			debugPrintChildPointers(e.Child)
		}
	}
}

func debugSiblingChild(n *Node) error {
	lastChild := false
	for _, e := range n.Entries {
		if e.IsChild() {
			if lastChild {
				return fmt.Errorf("neighboring children in entries list")
			}
			lastChild = true
		}
		if e.IsValue() {
			lastChild = false
		}
	}
	return nil
}
