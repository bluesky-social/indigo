package mst

import (
	"fmt"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/assert"
)

func TestBasicMST(t *testing.T) {
	assert := assert.New(t)

	c2, _ := cid.Decode("bafkreieqq463374bbcbeq7gpmet5rvrpeqow6t4rtjzrkhnlu222222222")
	c3, _ := cid.Decode("bafkreieqq463374bbcbeq7gpmet5rvrpeqow6t4rtjzrkhnlu333333333")
	assert.NotEmpty(c2)
	assert.NotEmpty(c3)
	tree := NewEmptyTree()

	tree, prev, err := Insert(tree, []byte("abc"), c2, -1)
	assert.NoError(err)
	assert.Empty(prev)

	assert.Equal(1, len(tree.Entries))

	val, err := Get(tree, []byte("abc"), -1)
	assert.NoError(err)
	assert.Equal(c2, *val)

	val, err = Get(tree, []byte("xyz"), -1)
	assert.NoError(err)
	assert.Empty(val)

	tree, prev, err = Insert(tree, []byte("abc"), c3, -1)
	assert.NoError(err)
	assert.NotEmpty(prev)
	assert.Equal(&c2, prev)

	val, err = Get(tree, []byte("abc"), -1)
	assert.NoError(err)
	assert.Equal(&c3, val)

	tree, prev, err = Insert(tree, []byte("aaa"), c2, -1)
	assert.NoError(err)
	assert.Empty(prev)

	tree, prev, err = Insert(tree, []byte("zzz"), c3, -1)
	assert.NoError(err)
	assert.Empty(prev)

	val, err = Get(tree, []byte("zzz"), -1)
	assert.NoError(err)
	assert.Equal(&c3, val)

	m := make(map[string]cid.Cid)
	assert.NoError(ReadTreeToMap(tree, m))
	fmt.Println("-----")
	DebugPrintMap(m)
	fmt.Println("-----")
	DebugPrintTree(tree, 0)

	tree, prev, err = Remove(tree, []byte("abc"), -1)
	assert.NoError(err)
	assert.NotEmpty(prev)
	assert.Equal(&c3, prev)
}

func TestBasicMap(t *testing.T) {
	assert := assert.New(t)

	c2, _ := cid.Decode("bafkreieqq463374bbcbeq7gpmet5rvrpeqow6t4rtjzrkhnlu222222222")
	c3, _ := cid.Decode("bafkreieqq463374bbcbeq7gpmet5rvrpeqow6t4rtjzrkhnlu333333333")
	assert.NotEmpty(c2)
	assert.NotEmpty(c3)

	inMap := map[string]cid.Cid{
		"a": c2,
		"b": c2,
		"c": c2,
		"d": c3,
		"e": c3,
		"f": c3,
		"g": c3,
	}

	tree, err := NewTreeFromMap(inMap)
	assert.NoError(err)

	outMap := make(map[string]cid.Cid, len(inMap))
	err = ReadTreeToMap(tree, outMap)
	assert.NoError(err)
	assert.Equal(inMap, outMap)
}
