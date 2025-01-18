package mst

import (
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
	if depth == 0 {
		fmt.Printf("tree root\n")
	}
	for i, e := range n.Entries {
		if depth > 0 && i == 0 {
			for range depth - 1 {
				fmt.Printf("│")
			}
			fmt.Printf("┬")
		} else {
			for range depth {
				fmt.Printf("│")
			}
			fmt.Printf("├")
		}
		if e.IsValue() {
			fmt.Printf(" %s -> %s (%d)\n", e.Key, e.Value, HeightForKey(e.Key))
		} else if e.IsPointer() {
			if e.Child != nil {
				DebugPrintTree(e.Child, depth+1)
			} else {
				fmt.Printf(" (partial) %s", e.ChildCID)
			}
		}
	}
}
