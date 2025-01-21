// This file contains tests which are the same across language implementations.
// AKA, if you update this file, you should probably update the corresponding
// file in atproto repo (typescript)
package mst

import (
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

func mapToMstRootCidString(t *testing.T, m map[string]string) string {

	tree, err := NewTreeFromMap(mapToCidMapDecode(t, m))
	if err != nil {
		t.Fatal(err)
	}

	c, err := ComputeCID(tree)
	if err != nil {
		t.Fatal(err)
	}

	return c.String()
}

// TODO: TestAllowedKeys

func TestManualNode(t *testing.T) {

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
				Value:       cid1,
				Right:      nil,
			},
		},
	}
	n := simple_nd.Node(nil)
	assert.Equal(t, simple_nd, n.NodeData())

	mcid, err := ComputeCID(&n)
	if err != nil {
		t.Fatal(err)
	}
	/* TODO
	block, err := bs.Get(ctx, mcid)
	if err != nil {
		t.Fatal(err)
	}
	if false {
		fmt.Printf("%#v\n", block)
	}
	*/
	assert.Equal(t, "bafyreibj4lsc3aqnrvphp5xmrnfoorvru4wynt6lwidqbm2623a6tatzdu", mcid.String())
}

func TestInteropKnownMaps(t *testing.T) {

	cid1str := "bafyreie5cvv4h45feadgeuwhbcutmh6t2ceseocckahdoe6uat64zmz454"

	// empty map
	emptyMap := map[string]string{}
	assert.Equal(t, "bafyreie5737gdxlw5i64vzichcalba3z2v5n6icifvx5xytvske7mr3hpm", mapToMstRootCidString(t, emptyMap))

	// no depth, single entry
	trivialMap := map[string]string{
		"com.example.record/3jqfcqzm3fo2j": cid1str,
	}
	assert.Equal(t, "bafyreibj4lsc3aqnrvphp5xmrnfoorvru4wynt6lwidqbm2623a6tatzdu", mapToMstRootCidString(t, trivialMap))

	// single layer=2 entry
	singlelayer2Map := map[string]string{
		"com.example.record/3jqfcqzm3fx2j": cid1str,
	}
	assert.Equal(t, "bafyreih7wfei65pxzhauoibu3ls7jgmkju4bspy4t2ha2qdjnzqvoy33ai", mapToMstRootCidString(t, singlelayer2Map))

	// pretty simple, but with some depth
	simpleMap := map[string]string{
		"com.example.record/3jqfcqzm3fp2j": cid1str,
		"com.example.record/3jqfcqzm3fr2j": cid1str,
		"com.example.record/3jqfcqzm3fs2j": cid1str,
		"com.example.record/3jqfcqzm3ft2j": cid1str,
		"com.example.record/3jqfcqzm4fc2j": cid1str,
	}
	assert.Equal(t, "bafyreicmahysq4n6wfuxo522m6dpiy7z7qzym3dzs756t5n7nfdgccwq7m", mapToMstRootCidString(t, simpleMap))
}

func TestInteropKnownMapsTricky(t *testing.T) {
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
	assert.Equal(t, "bafyreiecb33zh7r2sc3k2wthm6exwzfktof63kmajeildktqc25xj6qzx4", mapToMstRootCidString(t, trickyMap))
}

// "trims top of tree on delete"
func TestInteropEdgeCasesTrimTop(t *testing.T) {

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
	trimMst, err := NewTreeFromMap(mapToCidMapDecode(t, trimMap))
	if err != nil {
        t.Fatal(err)
    }
    trimBefore, err := ComputeCID(trimMst)
    if err != nil {
        t.Fatal(err)
    }
	assert.Equal(t, 1, trimMst.Height)
	assert.Equal(t, l1root, trimBefore.String())

	trimMst, _, err = Remove(trimMst, []byte("com.example.record/3jqfcqzm3fs2j"), -1) // level 1
	if err != nil {
		t.Fatal(err)
	}
    trimAfter, err := ComputeCID(trimMst)
    if err != nil {
        t.Fatal(err)
    }
	//fmt.Printf("%#v\n", trimMst)
	assert.Equal(t, 0, trimMst.Height)
	assert.Equal(t, l0root, trimAfter.String())
}

func TestInteropEdgeCasesInsertion(t *testing.T) {

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
	insertionMst, err := NewTreeFromMap(mapToCidMapDecode(t, insertionMap))
	if err != nil {
        t.Fatal(err)
    }
    insertionBefore, err := ComputeCID(insertionMst)
    if err != nil {
        t.Fatal(err)
    }
	assert.Equal(t, 1, insertionMst.Height)
	assert.Equal(t, l1root, insertionBefore.String())

	// insert F, which will push E out of the node with G+H to a new node under D
	insertionMst, _, err = Insert(insertionMst, []byte("com.example.record/3jqfcqzm3fx2j"), cid1, -1) // F; level 2
	if err != nil {
		t.Fatal(err)
	}
    insertionAfter, err := ComputeCID(insertionMst)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, 2, insertionMst.Height)
	assert.Equal(t, l2root, insertionAfter.String())

	// remove F, which should push E back over with G+H
	insertionMst, _, err = Remove(insertionMst, []byte("com.example.record/3jqfcqzm3fx2j"), -1) // F; level 2
	if err != nil {
		t.Fatal(err)
	}
    insertionFinal, err := ComputeCID(insertionMst)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, 1, insertionMst.Height)
	assert.Equal(t, l1root, insertionFinal.String())
}

// "handles new layers that are two higher than existing"
func TestInteropEdgeCasesHigher(t *testing.T) {

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
	higherMst, err := NewTreeFromMap(mapToCidMapDecode(t, higherMap))
	if err != nil {
		t.Fatal(err)
	}
	higherBefore, err := ComputeCID(higherMst)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, 0, higherMst.Height)
	assert.Equal(t, l0root, higherBefore.String())

	// insert B, which is two levels above
	higherMst, _, err = Insert(higherMst, []byte("com.example.record/3jqfcqzm3fx2j"), cid1, -1) // B; level 2
	if err != nil {
		t.Fatal(err)
	}
	higherAfter, err := ComputeCID(higherMst)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, l2root, higherAfter.String())

	// remove B
	higherMst, _, err = Remove(higherMst, []byte("com.example.record/3jqfcqzm3fx2j"), -1) // B; level 2
	if err != nil {
		t.Fatal(err)
	}
	higherAgain, err := ComputeCID(higherMst)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, 0, higherMst.Height)
	assert.Equal(t, l0root, higherAgain.String())

	// insert B (level=2) and D (level=1)
	higherMst, _, err = Insert(higherMst, []byte("com.example.record/3jqfcqzm3fx2j"), cid1, -1) // B; level 2
	if err != nil {
		t.Fatal(err)
	}
	higherMst, _, err = Insert(higherMst, []byte("com.example.record/3jqfcqzm4fd2j"), cid1, -1) // D; level 1
	if err != nil {
		t.Fatal(err)
	}
	higherYetAgain, err := ComputeCID(higherMst)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, 2, higherMst.Height)
	assert.Equal(t, l2root2, higherYetAgain.String())

	// remove D
	higherMst, _, err = Remove(higherMst, []byte("com.example.record/3jqfcqzm4fd2j"), -1) // D; level 1
	if err != nil {
		t.Fatal(err)
	}
	higherFinal, err := ComputeCID(higherMst)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, 2, higherMst.Height)
	assert.Equal(t, l2root, higherFinal.String())
}
