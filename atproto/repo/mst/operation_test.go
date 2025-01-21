package mst

import (
	"context"
	"fmt"
	"math/rand"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	"github.com/stretchr/testify/assert"
)

func TestBasicOperation(t *testing.T) {
	assert := assert.New(t)

	c2, _ := cid.Decode("bafkreieqq463374bbcbeq7gpmet5rvrpeqow6t4rtjzrkhnlu222222222")
	c3, _ := cid.Decode("bafkreieqq463374bbcbeq7gpmet5rvrpeqow6t4rtjzrkhnlu333333333")
	tree := NewEmptyTree()
	var op *Operation
	var err error

	tree, op, err = ApplyOp(tree, "color/green", &c2)
	assert.NoError(err)
	assert.True(op.IsCreate())
	assert.NoError(CheckOp(tree, op))

	tree, op, err = ApplyOp(tree, "color/brown", &c2)
	assert.NoError(err)
	assert.True(op.IsCreate())
	assert.NoError(CheckOp(tree, op))

	tree, op, err = ApplyOp(tree, "color/brown", &c3)
	assert.NoError(err)
	assert.True(op.IsUpdate())
	assert.Equal(c3, *op.Value)
	assert.Equal(c2, *op.Prev)
	assert.NoError(CheckOp(tree, op))

	tree, op, err = ApplyOp(tree, "color/brown", nil)
	assert.NoError(err)
	assert.True(op.IsDelete())
	assert.NoError(CheckOp(tree, op))
	tree, err = InvertOp(tree, op)
	assert.NoError(err)
	assert.Error(CheckOp(tree, op))

	tree, op, err = ApplyOp(tree, "color/orange", &c3)
	assert.NoError(err)
	assert.True(op.IsCreate())
	assert.NoError(CheckOp(tree, op))
	tree, err = InvertOp(tree, op)
	assert.NoError(err)
	assert.Error(CheckOp(tree, op))

	tree, op, err = ApplyOp(tree, "color/pink", &c3)
	assert.NoError(err)
	tree, op, err = ApplyOp(tree, "color/pink", &c2)
	assert.NoError(CheckOp(tree, op))
	assert.True(op.IsUpdate())
	tree, err = InvertOp(tree, op)
	assert.NoError(err)
	assert.Error(CheckOp(tree, op))
}

func TestRandomOperations(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()
	size := 1000
	opCount := 3
	iterations := 10

	// generate a random starting tree
	startMap := make(map[string]cid.Cid, size)
	for range size {
		k := randomStr()
		// ensure key is not already in the random set
		for {
			_, ok := startMap[k]
			if !ok {
				break
			}
			k = randomStr()
		}
		startMap[k] = randomCid()
	}
	mapKeys := make([]string, len(startMap))
	i := 0
	for k, _ := range startMap {
		mapKeys[i] = k
		i++
	}
	rand.Shuffle(len(mapKeys), func(i, j int) {
		mapKeys[i], mapKeys[j] = mapKeys[j], mapKeys[i]
	})
	tree, err := NewTreeFromMap(startMap)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(size, debugCountEntries(tree))
	assert.NoError(VerifyTreeStructure(tree, -1, nil))

	for range iterations {
		// compute CID of the tree
		startCID, err := NodeCID(tree)
		if err != nil {
			t.Fatal(err)
		}

		// do some random ops
		opSet := []Operation{}
		var op *Operation
		c := randomCid()
		for range opCount {
			// creations
			tree, op, err = ApplyOp(tree, randomStr(), &c)
			assert.NoError(err)
			opSet = append(opSet, *op)
		}

		for range opCount {
			// deletions
			tree, op, err = ApplyOp(tree, mapKeys[rand.Intn(len(mapKeys))], nil)
			assert.NoError(err)
			if op.Prev != nil {
				opSet = append(opSet, *op)
			}
		}

		for range opCount {
			// updates (must happen after deletions!)
			k := mapKeys[rand.Intn(len(mapKeys))]
			v, err := Get(tree, []byte(k), -1)
			assert.NoError(err)
			if v != nil && *v != c {
				tree, op, err = ApplyOp(tree, k, &c)
				assert.NoError(err)
				assert.Equal(*v, *op.Prev)
				assert.Equal(c, *op.Value)
				if op.Prev != nil {
					opSet = append(opSet, *op)
				}
			}
		}

		// check all ops against full tree (belt-and-suspenders)
		for _, op := range opSet {
			assert.NoError(CheckOp(tree, &op))
		}

		// extract diff as separate tree, and validate that
		diffBlocks := blockstore.NewBlockstore(datastore.NewMapDatastore())
		diffRoot, err := DiffNode(tree, diffBlocks)
		if err != nil {
			t.Fatal(err)
		}
		diffTree, err := HydrateNode(ctx, diffBlocks, *diffRoot)
		if err != nil {
			t.Fatal(err)
		}
		EnsureHeights(diffTree)
		assert.NoError(VerifyTreeStructure(diffTree, -1, nil))

		// re-compute partial commit (not related to main test path)
		diffCID, err := NodeCID(diffTree)
		if err != nil {
			t.Fatal(err)
		}
		assert.Equal(*diffRoot, *diffCID)

		// XXX: try inverting on full tree
		diffTree = tree

		// invert all the ops
		for i, op := range opSet {
			err := CheckOp(diffTree, &op)
			fmt.Printf("loop=%d key=%s val=%s prev=%s\n", i, op.Path, op.Value, op.Prev)
			assert.NoError(VerifyTreeStructure(diffTree, -1, nil))
			if err != nil {
				//debugPrintTree(diffTree, 0)
				t.Fatal(err)
			}

			diffTree, err = InvertOp(diffTree, &op)
			assert.NoError(err)
			if err != nil {
				t.Fatal(err)
			}
		}

		finalCID, err := NodeCID(diffTree)
		if err != nil {
			t.Fatal(err)
		}
		assert.Equal(*startCID, *finalCID)
	}
}
