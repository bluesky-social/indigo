// This file contains tests which are the same across language implementations.
// AKA, if you update this file, you should probably update the corresponding
// file in atproto repo (typescript)
package mst

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ipfs/go-cid"
)

func mapToCidMapDecode(t *testing.T, a map[string]string) map[string]cid.Cid {
	out := make(map[string]cid.Cid)
	for k, v := range a {
		c, err := cid.Decode(v)
		if err != nil {
			t.Fatal(err)
		}
		out[k] = c
	}
	return out
}

func mapToTreeRootCidString(t *testing.T, m map[string]string) string {

	tree, err := LoadTreeFromMap(mapToCidMapDecode(t, m))
	if err != nil {
		t.Fatal(err)
	}

	c, err := tree.RootCID()
	if err != nil {
		t.Fatal(err)
	}

	return c.String()
}

// TODO: TestAllowedKeys

func TestManualNode(t *testing.T) {
	assert := assert.New(t)

	cid1, err := cid.Decode("bafyreie5cvv4h45feadgeuwhbcutmh6t2ceseocckahdoe6uat64zmz454")
	if err != nil {
		t.Fatal(err)
	}

	simple_nd := NodeData{
		Left: nil,
		Entries: []EntryData{
			{
				PrefixLen: 0,
				KeySuffix: []byte("com.example.record/3jqfcqzm3fo2j"),
				Value:     cid1,
				Right:     nil,
			},
		},
	}
	n := simple_nd.Node(nil)
	assert.Equal(simple_nd, n.NodeData())

	mcid, err := n.writeBlocks(context.Background(), nil, true)
	if err != nil {
		t.Fatal(err)
	}
	assert.NoError(err)
	assert.Equal("bafyreibj4lsc3aqnrvphp5xmrnfoorvru4wynt6lwidqbm2623a6tatzdu", mcid.String())
}

func TestInteropKnownMaps(t *testing.T) {
	assert := assert.New(t)

	cid1str := "bafyreie5cvv4h45feadgeuwhbcutmh6t2ceseocckahdoe6uat64zmz454"

	// empty map
	emptyMap := map[string]string{}
	assert.Equal("bafyreie5737gdxlw5i64vzichcalba3z2v5n6icifvx5xytvske7mr3hpm", mapToTreeRootCidString(t, emptyMap))

	// no depth, single entry
	trivialMap := map[string]string{
		"com.example.record/3jqfcqzm3fo2j": cid1str,
	}
	assert.Equal("bafyreibj4lsc3aqnrvphp5xmrnfoorvru4wynt6lwidqbm2623a6tatzdu", mapToTreeRootCidString(t, trivialMap))

	// single layer=2 entry
	singlelayer2Map := map[string]string{
		"com.example.record/3jqfcqzm3fx2j": cid1str,
	}
	assert.Equal("bafyreih7wfei65pxzhauoibu3ls7jgmkju4bspy4t2ha2qdjnzqvoy33ai", mapToTreeRootCidString(t, singlelayer2Map))

	// pretty simple, but with some depth
	simpleMap := map[string]string{
		"com.example.record/3jqfcqzm3fp2j": cid1str,
		"com.example.record/3jqfcqzm3fr2j": cid1str,
		"com.example.record/3jqfcqzm3fs2j": cid1str,
		"com.example.record/3jqfcqzm3ft2j": cid1str,
		"com.example.record/3jqfcqzm4fc2j": cid1str,
	}
	assert.Equal("bafyreicmahysq4n6wfuxo522m6dpiy7z7qzym3dzs756t5n7nfdgccwq7m", mapToTreeRootCidString(t, simpleMap))
}

func TestInteropKnownMapsTricky(t *testing.T) {
	assert := assert.New(t)

	t.Skip("TODO: these are currently disallowed in typescript implementation")

	cid1str := "bafyreie5cvv4h45feadgeuwhbcutmh6t2ceseocckahdoe6uat64zmz454"

	// include several known edge cases
	trickyMap := map[string]string{
		"":            cid1str,
		"jalapeño":    cid1str,
		"coöperative": cid1str,
		"coüperative": cid1str,
		"abc\x00":     cid1str,
	}
	assert.Equal("bafyreiecb33zh7r2sc3k2wthm6exwzfktof63kmajeildktqc25xj6qzx4", mapToTreeRootCidString(t, trickyMap))
}

// "trims top of tree on delete"
func TestInteropEdgeCasesTrimTop(t *testing.T) {
	assert := assert.New(t)

	cid1str := "bafyreie5cvv4h45feadgeuwhbcutmh6t2ceseocckahdoe6uat64zmz454"
	l1root := "bafyreifnqrwbk6ffmyaz5qtujqrzf5qmxf7cbxvgzktl4e3gabuxbtatv4"
	l0root := "bafyreie4kjuxbwkhzg2i5dljaswcroeih4dgiqq6pazcmunwt2byd725vi"

	trimMap := map[string]string{
		"com.example.record/3jqfcqzm3fn2j": cid1str, // level 0
		"com.example.record/3jqfcqzm3fo2j": cid1str, // level 0
		"com.example.record/3jqfcqzm3fp2j": cid1str, // level 0
		"com.example.record/3jqfcqzm3fs2j": cid1str, // level 0
		"com.example.record/3jqfcqzm3ft2j": cid1str, // level 0
		"com.example.record/3jqfcqzm3fu2j": cid1str, // level 1
	}
	trimTree, err := LoadTreeFromMap(mapToCidMapDecode(t, trimMap))
	if err != nil {
		t.Fatal(err)
	}
	trimBefore, err := trimTree.RootCID()
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(1, trimTree.Root.Height)
	assert.Equal(l1root, trimBefore.String())

	_, err = trimTree.Remove([]byte("com.example.record/3jqfcqzm3fs2j")) // level 1
	if err != nil {
		t.Fatal(err)
	}
	trimAfter, err := trimTree.RootCID()
	if err != nil {
		t.Fatal(err)
	}
	//fmt.Printf("%#v\n", trimTree)
	assert.Equal(0, trimTree.Root.Height)
	assert.Equal(l0root, trimAfter.String())
}

func TestInteropEdgeCasesInsertion(t *testing.T) {
	assert := assert.New(t)

	cid1str := "bafyreie5cvv4h45feadgeuwhbcutmh6t2ceseocckahdoe6uat64zmz454"
	cid1, err := cid.Decode(cid1str)
	if err != nil {
		t.Fatal(err)
	}

	// "handles insertion that splits two layers down"
	l1root := "bafyreiettyludka6fpgp33stwxfuwhkzlur6chs4d2v4nkmq2j3ogpdjem"
	l2root := "bafyreid2x5eqs4w4qxvc5jiwda4cien3gw2q6cshofxwnvv7iucrmfohpm"
	insertionMap := map[string]string{
		"com.example.record/3jqfcqzm3fo2j": cid1str, // A; level 0
		"com.example.record/3jqfcqzm3fp2j": cid1str, // B; level 0
		"com.example.record/3jqfcqzm3fr2j": cid1str, // C; level 0
		"com.example.record/3jqfcqzm3fs2j": cid1str, // D; level 1
		"com.example.record/3jqfcqzm3ft2j": cid1str, // E; level 0
		"com.example.record/3jqfcqzm3fz2j": cid1str, // G; level 0
		"com.example.record/3jqfcqzm4fc2j": cid1str, // H; level 0
		"com.example.record/3jqfcqzm4fd2j": cid1str, // I; level 1
		"com.example.record/3jqfcqzm4ff2j": cid1str, // J; level 0
		"com.example.record/3jqfcqzm4fg2j": cid1str, // K; level 0
		"com.example.record/3jqfcqzm4fh2j": cid1str, // L; level 0
	}
	insertionTree, err := LoadTreeFromMap(mapToCidMapDecode(t, insertionMap))
	if err != nil {
		t.Fatal(err)
	}
	insertionBefore, err := insertionTree.RootCID()
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(1, insertionTree.Root.Height)
	assert.Equal(l1root, insertionBefore.String())

	// insert F, which will push E out of the node with G+H to a new node under D
	_, err = insertionTree.Insert([]byte("com.example.record/3jqfcqzm3fx2j"), cid1) // F; level 2
	if err != nil {
		t.Fatal(err)
	}
	insertionAfter, err := insertionTree.RootCID()
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(2, insertionTree.Root.Height)
	assert.Equal(l2root, insertionAfter.String())

	// remove F, which should push E back over with G+H
	_, err = insertionTree.Remove([]byte("com.example.record/3jqfcqzm3fx2j")) // F; level 2
	if err != nil {
		t.Fatal(err)
	}
	insertionFinal, err := insertionTree.RootCID()
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(1, insertionTree.Root.Height)
	assert.Equal(l1root, insertionFinal.String())
}

// "handles new layers that are two higher than existing"
func TestInteropEdgeCasesHigher(t *testing.T) {
	assert := assert.New(t)

	cid1str := "bafyreie5cvv4h45feadgeuwhbcutmh6t2ceseocckahdoe6uat64zmz454"
	cid1, err := cid.Decode(cid1str)
	if err != nil {
		t.Fatal(err)
	}

	l0root := "bafyreidfcktqnfmykz2ps3dbul35pepleq7kvv526g47xahuz3rqtptmky"
	l2root := "bafyreiavxaxdz7o7rbvr3zg2liox2yww46t7g6hkehx4i4h3lwudly7dhy"
	l2root2 := "bafyreig4jv3vuajbsybhyvb7gggvpwh2zszwfyttjrj6qwvcsp24h6popu"
	higherMap := map[string]string{
		"com.example.record/3jqfcqzm3ft2j": cid1str, // A; level 0
		"com.example.record/3jqfcqzm3fz2j": cid1str, // C; level 0
	}
	higherTree, err := LoadTreeFromMap(mapToCidMapDecode(t, higherMap))
	if err != nil {
		t.Fatal(err)
	}
	higherBefore, err := higherTree.RootCID()
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(0, higherTree.Root.Height)
	assert.Equal(l0root, higherBefore.String())

	// insert B, which is two levels above
	_, err = higherTree.Insert([]byte("com.example.record/3jqfcqzm3fx2j"), cid1) // B; level 2
	if err != nil {
		t.Fatal(err)
	}
	higherAfter, err := higherTree.RootCID()
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(l2root, higherAfter.String())
	//debugPrintTree(higherTree, 0)

	// remove B
	_, err = higherTree.Remove([]byte("com.example.record/3jqfcqzm3fx2j")) // B; level 2
	if err != nil {
		t.Fatal(err)
	}
	higherAgain, err := higherTree.RootCID()
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(0, higherTree.Root.Height)
	assert.Equal(l0root, higherAgain.String())

	// insert B (level=2) and D (level=1)
	_, err = higherTree.Insert([]byte("com.example.record/3jqfcqzm3fx2j"), cid1) // B; level 2
	if err != nil {
		t.Fatal(err)
	}
	_, err = higherTree.Insert([]byte("com.example.record/3jqfcqzm4fd2j"), cid1) // D; level 1
	if err != nil {
		t.Fatal(err)
	}
	higherYetAgain, err := higherTree.RootCID()
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(2, higherTree.Root.Height)
	assert.Equal(l2root2, higherYetAgain.String())
	assert.NoError(higherTree.Verify())
	//debugPrintTree(higherTree, 0)

	// remove D
	_, err = higherTree.Remove([]byte("com.example.record/3jqfcqzm4fd2j")) // D; level 1
	if err != nil {
		t.Fatal(err)
	}
	higherFinal, err := higherTree.RootCID()
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(2, higherTree.Root.Height)
	assert.Equal(l2root, higherFinal.String())
	assert.NoError(higherTree.Verify())
	//debugPrintTree(higherTree, 0)
}
