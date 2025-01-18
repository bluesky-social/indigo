package mst

import (
	"encoding/hex"
	"fmt"
	"math/rand"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
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
	// XXX: DebugPrintMap(m)
	fmt.Println("-----")
	// XXX: DebugPrintTree(tree, 0)

	tree, prev, err = Remove(tree, []byte("abc"), -1)
	assert.NoError(err)
	assert.NotEmpty(prev)
	assert.Equal(&c3, prev)

	assert.NoError(DebugTreeStructure(tree, -1, nil))
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
		"h": c3,
		"i": c3,
	}

	tree, err := NewTreeFromMap(inMap)
	assert.NoError(err)

	// XXX: fmt.Println("-----")
	// XXX: DebugPrintTree(tree, 0)
	assert.NoError(DebugTreeStructure(tree, -1, nil))

	outMap := make(map[string]cid.Cid, len(inMap))
	err = ReadTreeToMap(tree, outMap)
	assert.NoError(err)
	assert.Equal(inMap, outMap)
}

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
	buf := make([]byte, 6)
	rand.Read(buf)
	return hex.EncodeToString(buf)
}

func TestRandomTree(t *testing.T) {
	assert := assert.New(t)

	// XXX: size := 500
	size := 1

	inMap := make(map[string]cid.Cid, size)
	outMap := make(map[string]cid.Cid, size)

	for range size {
		k := randomStr()
		// ensure key is not already in the random set
		for {
			_, ok := inMap[k]
			if !ok {
				break
			}
			k = randomStr()
		}
		inMap[randomStr()] = randomCid()
	}

	tree, err := NewTreeFromMap(inMap)
	assert.NoError(err)

	fmt.Println("-----")
	// XXX: DebugPrintTree(tree, 0)
	assert.NoError(DebugTreeStructure(tree, -1, nil))
	assert.Equal(size, DebugCountEntries(tree))

	err = ReadTreeToMap(tree, outMap)
	assert.NoError(err)
	assert.Equal(len(inMap), len(outMap))
	// XXX: assert.Equal(inMap, outMap)
}

func TestRandomUntilError(t *testing.T) {
	assert := assert.New(t)
	var err error
	var prev *cid.Cid

	// XXX: size := 100
	size := 1

	tree := NewEmptyTree()
	count := 0
	fmt.Println("-----")
	for range size {
		key := []byte(randomStr())
		val := randomCid()
		fmt.Printf("%s %s\n", key, val)
		tree, prev, err = Insert(tree, key, val, -1)
		assert.NoError(err)
		if prev == nil {
			count++
		}

		assert.Equal(count, DebugCountEntries(tree))
		err = DebugTreeStructure(tree, -1, nil)
		assert.NoError(err)
		if err != nil || count != DebugCountEntries(tree) {
			fmt.Println("-----")
			DebugPrintTree(tree, 0)
			break
		}
	}
}

func TestBrokenCaseOne(t *testing.T) {
	assert := assert.New(t)
	var err error

	entries := [][]string{
		{"1ea173efefa4", "bafkreibey6qzs7vb4wzlzfo7flflevl7qstzaggooiqivuexb6snapadq4"},
		{"bed5c5789108", "bafkreifoxw552rsnuoargsfilhwmhprxr6qyzjmbtgjzmboii4x4mk4aoi"},
		{"340b57a94d4c", "bafkreigarcm3fvnekjml6vmm5dyg46qnfkpc2lhghnh2wvntwvbrvxzq7q"},
		{"8d37e30d3d29", "bafkreifdgiz7dmgng4aebiw5m6w4cypiiar2edgtkkfg47o3pniir3pxve"},
		{"ee4b5efda333", "bafkreiho7qtewg7fm7egxe2ectkm2ykqygakph3nt4rrlp5mxwkvdwckk4"},
		{"1180aeeadc01", "bafkreifqhtleufnxv2nkwehoa5lgmilwgkfqvlpkwbalvka6m6675ewkhu"},
		{"c368b6b55998", "bafkreial4xepr5wnhetxnkmylmipdmjybxsgf74becdi74olmzb5w5gpiq"},
		{"b948d2e0fc76", "bafkreiaefdmlyfjf4qovfyn22zbpw57wu667jtrvavogfxr7drewx4u24y"},
		{"93c53d491ffd", "bafkreie2nxdmjsy6k6lendnsy7bzyufj7j37l42ymquwmpuzsauraqsibq"},
		//{"54ef0958a374", "bafkreigbnjxc7wbxgqxs2n2djjmlxnuf222gdiq4jgdtkse4yn67v5crq4"},
	}

	tree := NewEmptyTree()
	for _, row := range entries {
		fmt.Println("-----")
		val, _ := cid.Decode(row[1])
		tree, _, err = Insert(tree, []byte(row[0]), val, -1)
		assert.NoError(err)
		if row[0] == "93c53d491ffd" {
			fmt.Println("-----")
			DebugPrintNodePointers(tree)
		}
	}

	fmt.Println("-----")
	DebugPrintNodePointers(tree)
	DebugPrintTree(tree, 0)
	assert.Equal(len(entries), DebugCountEntries(tree))
	assert.NoError(DebugTreeStructure(tree, -1, nil))
	assert.Error(nil)
}
