package repo

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/rand"
	"testing"

	"github.com/bluesky-social/indigo/atproto/repo/mst"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/assert"
)

func randomCid() cid.Cid {
	buf := make([]byte, 32)
	rand.Read(buf)
	c, err := cid.NewPrefixV1(cid.Raw, multihash.SHA2_256).Sum(buf)
	if err != nil {
		panic(err)
	}
	return c
}

func randomStr() string {
	buf := make([]byte, 16)
	rand.Read(buf)
	return hex.EncodeToString(buf)
}

func debugCountEntries(n *mst.Node) int {
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

func TestBasicOperation(t *testing.T) {
	assert := assert.New(t)

	c2, _ := cid.Decode("bafkreieqq463374bbcbeq7gpmet5rvrpeqow6t4rtjzrkhnlu222222222")
	c3, _ := cid.Decode("bafkreieqq463374bbcbeq7gpmet5rvrpeqow6t4rtjzrkhnlu333333333")
	et := mst.NewEmptyTree()
	tree := &et
	var op *Operation
	var err error

	op, err = ApplyOp(tree, "color/green", &c2)
	assert.NoError(err)
	assert.True(op.IsCreate())
	assert.NoError(CheckOp(tree, op))

	op, err = ApplyOp(tree, "color/brown", &c2)
	assert.NoError(err)
	assert.True(op.IsCreate())
	assert.NoError(CheckOp(tree, op))

	op, err = ApplyOp(tree, "color/brown", &c3)
	assert.NoError(err)
	assert.True(op.IsUpdate())
	assert.Equal(c3, *op.Value)
	assert.Equal(c2, *op.Prev)
	assert.NoError(CheckOp(tree, op))

	op, err = ApplyOp(tree, "color/brown", nil)
	assert.NoError(err)
	assert.True(op.IsDelete())
	assert.NoError(CheckOp(tree, op))
	err = InvertOp(tree, op)
	assert.NoError(err)
	assert.Error(CheckOp(tree, op))

	op, err = ApplyOp(tree, "color/orange", &c3)
	assert.NoError(err)
	assert.True(op.IsCreate())
	assert.NoError(CheckOp(tree, op))
	err = InvertOp(tree, op)
	assert.NoError(err)
	assert.Error(CheckOp(tree, op))

	op, err = ApplyOp(tree, "color/pink", &c3)
	assert.NoError(err)
	op, err = ApplyOp(tree, "color/pink", &c2)
	assert.NoError(CheckOp(tree, op))
	assert.True(op.IsUpdate())
	err = InvertOp(tree, op)
	assert.NoError(err)
	assert.Error(CheckOp(tree, op))
}

func TestRandomOperations(t *testing.T) {
	// single-op commits, near-empty repo
	randomOperations(t, 1, 1, 50)
	// single-op commits, large repo
	randomOperations(t, 10000, 1, 50)
	// multi-op commit
	randomOperations(t, 2000, 8, 50)
}

func randomOperations(t *testing.T, size, opCount, iterations int) {
	assert := assert.New(t)
	ctx := context.Background()

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
	tree, err := mst.LoadTreeFromMap(startMap)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(size, debugCountEntries(tree.Root))
	assert.NoError(tree.Verify())

	for range iterations {
		// compute CID of the tree
		startCID, err := tree.RootCID()
		if err != nil {
			t.Fatal(err)
		}

		// do some random ops
		opSet := []Operation{}
		var op *Operation
		c := randomCid()
		for range opCount {
			// creations
			op, err = ApplyOp(tree, randomStr(), &c)
			assert.NoError(err)
			opSet = append(opSet, *op)
		}

		for range opCount {
			// deletions
			op, err = ApplyOp(tree, mapKeys[rand.Intn(len(mapKeys))], nil)
			assert.NoError(err)
			if op.Prev != nil {
				opSet = append(opSet, *op)
			}
		}

		for range opCount {
			// updates (must happen after deletions!)
			k := mapKeys[rand.Intn(len(mapKeys))]
			v, err := tree.Get([]byte(k))
			assert.NoError(err)
			if v != nil && *v != c {
				op, err = ApplyOp(tree, k, &c)
				assert.NoError(err)
				assert.Equal(*v, *op.Prev)
				assert.Equal(c, *op.Value)
				if op.Prev != nil {
					opSet = append(opSet, *op)
				}
			}
		}

		// extract diff as separate tree, and validate that
		diffBlocks := blockstore.NewBlockstore(datastore.NewMapDatastore())
		diffRoot, err := tree.WriteDiffBlocks(ctx, diffBlocks)
		if err != nil {
			t.Fatal(err)
		}
		diffTree, err := mst.LoadTreeFromStore(ctx, diffBlocks, *diffRoot)
		if err != nil {
			t.Fatal(err)
		}
		assert.NoError(tree.Verify())

		// re-compute partial commit (not related to main test path)
		diffCID, err := tree.RootCID()
		if err != nil {
			t.Fatal(err)
		}
		assert.Equal(*diffRoot, *diffCID)

		// uncomment this to try inverting on full tree
		//diffTree = tree

		// check all ops against full tree (this is a redundant check)
		for _, op := range opSet {
			assert.NoError(CheckOp(tree, &op))
		}

		// sort ops (comment to disable)
		opSet, err = NormalizeOps(opSet)
		if err != nil {
			t.Fatal(err)
		}

		// invert operations
		for i, op := range opSet {
			err := CheckOp(diffTree, &op)
			fmt.Printf("loop=%d key=%s val=%s prev=%s\n", i, op.Path, op.Value, op.Prev)
			assert.NoError(diffTree.Verify())
			if err != nil {
				//debugPrintTree(diffTree, 0)
				t.Fatal(err)
			}

			err = InvertOp(diffTree, &op)
			assert.NoError(err)
			if err != nil {
				t.Fatal(err)
			}
		}

		finalCID, err := diffTree.RootCID()
		if err != nil {
			t.Fatal(err)
		}
		assert.Equal(*startCID, *finalCID)
	}

	// fiddle this to purge test cache
	_ = 12
}

func TestNormalizeOps(t *testing.T) {
	assert := assert.New(t)

	c2, _ := cid.Decode("bafkreieqq463374bbcbeq7gpmet5rvrpeqow6t4rtjzrkhnlu222222222")
	c3, _ := cid.Decode("bafkreieqq463374bbcbeq7gpmet5rvrpeqow6t4rtjzrkhnlu333333333")

	simple := []Operation{
		Operation{
			Path:  "create-BBB",
			Value: &c2,
			Prev:  nil,
		},
		Operation{
			Path:  "create-AAA",
			Value: &c2,
			Prev:  nil,
		},
		Operation{
			Path:  "delete-me",
			Value: nil,
			Prev:  &c2,
		},
	}
	out, err := NormalizeOps(simple)
	assert.NoError(err)
	assert.Equal(3, len(out))
	assert.Equal("delete-me", out[0].Path)
	assert.Equal("create-BBB", out[2].Path)

	dupes := []Operation{
		Operation{
			Path:  "create-BBB",
			Value: nil,
			Prev:  &c2,
		},
		Operation{
			Path:  "create-BBB",
			Value: nil,
			Prev:  &c3,
		},
	}
	_, err = NormalizeOps(dupes)
	assert.Error(err)
}
