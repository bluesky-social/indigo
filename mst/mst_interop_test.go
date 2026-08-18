// This file contains tests which are the same across language implementations.
// AKA, if you update this file, you should probably update the corresponding
// file in atproto repo (typescript)
package mst

import (
	"context"
	"fmt"
	"testing"

	"github.com/bluesky-social/indigo/util"
	"github.com/stretchr/testify/assert"

	"github.com/ipfs/go-cid"
)

func TestLeadingZeros(t *testing.T) {
	msg := "MST 'depth' computation (SHA-256 leading zeros)"
	assert.Equal(t, leadingZerosOnHash(""), 0, msg)
	assert.Equal(t, leadingZerosOnHash("asdf"), 0, msg)
	assert.Equal(t, leadingZerosOnHash("blue"), 1, msg)
	assert.Equal(t, leadingZerosOnHash("2653ae71"), 0, msg)
	assert.Equal(t, leadingZerosOnHash("88bfafc7"), 2, msg)
	assert.Equal(t, leadingZerosOnHash("2a92d355"), 4, msg)
	assert.Equal(t, leadingZerosOnHash("884976f5"), 6, msg)
	assert.Equal(t, leadingZerosOnHash("app.bsky.feed.post/454397e440ec"), 4, msg)
	assert.Equal(t, leadingZerosOnHash("app.bsky.feed.post/9adeb165882c"), 8, msg)
}

func TestPrefixLen(t *testing.T) {
	msg := "length of common prefix between strings"
	assert.Equal(t, countPrefixLen("abc", "abc"), 3, msg)
	assert.Equal(t, countPrefixLen("", "abc"), 0, msg)
	assert.Equal(t, countPrefixLen("abc", ""), 0, msg)
	assert.Equal(t, countPrefixLen("ab", "abc"), 2, msg)
	assert.Equal(t, countPrefixLen("abc", "ab"), 2, msg)
	assert.Equal(t, countPrefixLen("abcde", "abc"), 3, msg)
	assert.Equal(t, countPrefixLen("abc", "abcde"), 3, msg)
	assert.Equal(t, countPrefixLen("abcde", "abc1"), 3, msg)
	assert.Equal(t, countPrefixLen("abcde", "abb"), 2, msg)
	assert.Equal(t, countPrefixLen("abcde", "qbb"), 0, msg)
	assert.Equal(t, countPrefixLen("abc", "abc\x00"), 3, msg)
	assert.Equal(t, countPrefixLen("abc\x00", "abc"), 3, msg)
}

func TestPrefixLenWide(t *testing.T) {
	// NOTE: these are not cross-language consistent!
	msg := "length of common prefix between strings (wide chars)"
	assert.Equal(t, len("jalapeño"), 9, msg) // 8 in javascript
	assert.Equal(t, len("💩"), 4, msg)        // 2 in javascript
	assert.Equal(t, len("👩‍👧‍👧"), 18, msg)   // 8 in javascript

	// many of the below are different in JS
	assert.Equal(t, countPrefixLen("jalapeño", "jalapeno"), 6, msg)
	assert.Equal(t, countPrefixLen("jalapeñoA", "jalapeñoB"), 9, msg)
	assert.Equal(t, countPrefixLen("coöperative", "coüperative"), 3, msg)
	assert.Equal(t, countPrefixLen("abc💩abc", "abcabc"), 3, msg)
	assert.Equal(t, countPrefixLen("💩abc", "💩ab"), 6, msg)
	assert.Equal(t, countPrefixLen("abc👩‍👦‍👦de", "abc👩‍👧‍👧de"), 13, msg)
}

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
	bs := memBs()
	ctx := context.Background()
	mst := cidMapToMst(t, bs, mapToCidMapDecode(t, m))
	ncid, err := mst.GetPointer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return ncid.String()
}

func TestAllowedKeys(t *testing.T) {

	bs := memBs()
	ctx := context.Background()
	cid1str := "bafyreie5cvv4h45feadgeuwhbcutmh6t2ceseocckahdoe6uat64zmz454"
	cid1, err := cid.Decode(cid1str)
	if err != nil {
		t.Fatal(err)
	}

	empty := map[string]string{}
	mst := cidMapToMst(t, bs, mapToCidMapDecode(t, empty))

	// rejects empty key
	_, err = mst.Add(ctx, "", cid1, -1)
	assert.NotNil(t, err)

	// rejects a key with no collection
	_, err = mst.Add(ctx, "asdf", cid1, -1)
	assert.NotNil(t, err)

	// rejects a key with a nested collection
	_, err = mst.Add(ctx, "nested/collection/asdf", cid1, -1)
	assert.NotNil(t, err)

	// rejects on empty coll or rkey
	_, err = mst.Add(ctx, "coll/", cid1, -1)
	assert.NotNil(t, err)
	_, err = mst.Add(ctx, "/rkey", cid1, -1)
	assert.NotNil(t, err)

	// rejects non-ascii chars
	_, err = mst.Add(ctx, "coll/jalapeñoA", cid1, -1)
	assert.NotNil(t, err)
	_, err = mst.Add(ctx, "coll/coöperative", cid1, -1)
	assert.NotNil(t, err)
	_, err = mst.Add(ctx, "coll/abc💩", cid1, -1)
	assert.NotNil(t, err)

	// rejects ascii that we dont support
	_, err = mst.Add(ctx, "coll/key$", cid1, -1)
	assert.NotNil(t, err)
	_, err = mst.Add(ctx, "coll/key%", cid1, -1)
	assert.NotNil(t, err)
	_, err = mst.Add(ctx, "coll/key(", cid1, -1)
	assert.NotNil(t, err)
	_, err = mst.Add(ctx, "coll/key)", cid1, -1)
	assert.NotNil(t, err)
	_, err = mst.Add(ctx, "coll/key+", cid1, -1)
	assert.NotNil(t, err)
	_, err = mst.Add(ctx, "coll/key=", cid1, -1)
	assert.NotNil(t, err)

	// rejects keys over 256 chars
	_, err = mst.Add(ctx, "coll/asdofiupoiwqeurfpaosidfuapsodirupasoirupasoeiruaspeoriuaspeoriu2p3o4iu1pqw3oiuaspdfoiuaspdfoiuasdfpoiasdufpwoieruapsdofiuaspdfoiuasdpfoiausdfpoasidfupasodifuaspdofiuasdpfoiasudfpoasidfuapsodfiuasdpfoiausdfpoasidufpasodifuapsdofiuasdpofiuasdfpoaisdufpao", cid1, -1)
	assert.NotNil(t, err)

	// allows long key under 256 chars
	_, err = mst.Add(ctx, "coll/asdofiupoiwqeurfpaosidfuapsodirupasoirupasoeiruaspeoriuaspeoriu2p3o4iu1pqw3oiuaspdfoiuaspdfoiuasdfpoiasdufpwoieruapsdofiuaspdfoiuasdpfoiausdfpoasidfupasodifuaspdofiuasdpfoiasudfpoasidfuapsodfiuasdpfoiausdfpoasidufpasodifuapsdofiuasdpofiuasdfpoaisduf", cid1, -1)
	assert.Nil(t, err)

	// allows URL-safe chars
	_, err = mst.Add(ctx, "coll/key0", cid1, -1)
	assert.Nil(t, err)
	_, err = mst.Add(ctx, "coll/key_", cid1, -1)
	assert.Nil(t, err)
	_, err = mst.Add(ctx, "coll/key:", cid1, -1)
	assert.Nil(t, err)
	_, err = mst.Add(ctx, "coll/key.", cid1, -1)
	assert.Nil(t, err)
	_, err = mst.Add(ctx, "coll/key-", cid1, -1)
	assert.Nil(t, err)

}

func TestManualNode(t *testing.T) {

	bs := memBs()
	cst := util.CborStore(bs)
	cid1, err := cid.Decode("bafyreie5cvv4h45feadgeuwhbcutmh6t2ceseocckahdoe6uat64zmz454")
	if err != nil {
		t.Fatal(err)
	}

	simple_nd := NodeData{
		Left: nil,
		Entries: []TreeEntry{
			{
				PrefixLen: 0,
				KeySuffix: []byte("com.example.record/3jqfcqzm3fo2j"),
				Val:       cid1,
				Tree:      nil,
			},
		},
	}
	mcid, err := cst.Put(context.TODO(), &simple_nd)
	if err != nil {
		t.Fatal(err)
	}
	block, err := bs.Get(context.TODO(), mcid)
	if err != nil {
		t.Fatal(err)
	}
	if false {
		fmt.Printf("%#v\n", block)
	}
	assert.Equal(t, mcid.String(), "bafyreibj4lsc3aqnrvphp5xmrnfoorvru4wynt6lwidqbm2623a6tatzdu")

}

func TestInteropKnownMaps(t *testing.T) {

	cid1str := "bafyreie5cvv4h45feadgeuwhbcutmh6t2ceseocckahdoe6uat64zmz454"

	// empty map
	emptyMap := map[string]string{}
	assert.Equal(t, mapToMstRootCidString(t, emptyMap), "bafyreie5737gdxlw5i64vzichcalba3z2v5n6icifvx5xytvske7mr3hpm")

	// no depth, single entry
	trivialMap := map[string]string{
		"com.example.record/3jqfcqzm3fo2j": cid1str,
	}
	assert.Equal(t, mapToMstRootCidString(t, trivialMap), "bafyreibj4lsc3aqnrvphp5xmrnfoorvru4wynt6lwidqbm2623a6tatzdu")

	// single layer=2 entry
	singlelayer2Map := map[string]string{
		"com.example.record/3jqfcqzm3fx2j": cid1str,
	}
	assert.Equal(t, mapToMstRootCidString(t, singlelayer2Map), "bafyreih7wfei65pxzhauoibu3ls7jgmkju4bspy4t2ha2qdjnzqvoy33ai")

	// pretty simple, but with some depth
	simpleMap := map[string]string{
		"com.example.record/3jqfcqzm3fp2j": cid1str,
		"com.example.record/3jqfcqzm3fr2j": cid1str,
		"com.example.record/3jqfcqzm3fs2j": cid1str,
		"com.example.record/3jqfcqzm3ft2j": cid1str,
		"com.example.record/3jqfcqzm4fc2j": cid1str,
	}
	assert.Equal(t, mapToMstRootCidString(t, simpleMap), "bafyreicmahysq4n6wfuxo522m6dpiy7z7qzym3dzs756t5n7nfdgccwq7m")
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
	assert.Equal(t, mapToMstRootCidString(t, trickyMap), "bafyreiecb33zh7r2sc3k2wthm6exwzfktof63kmajeildktqc25xj6qzx4")
}

// "trims top of tree on delete"
func TestInteropEdgeCasesTrimTop(t *testing.T) {

	bs := memBs()
	ctx := context.Background()
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
	trimMst := cidMapToMst(t, bs, mapToCidMapDecode(t, trimMap))
	trimBefore, err := trimMst.GetPointer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, trimMst.layer, 1)
	assert.Equal(t, trimBefore.String(), l1root)

	trimMst, err = trimMst.Delete(ctx, "com.example.record/3jqfcqzm3fs2j") // level 1
	if err != nil {
		t.Fatal(err)
	}
	trimAfter, err := trimMst.GetPointer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("%#v\n", trimMst)
	assert.Equal(t, trimMst.layer, 0)
	assert.Equal(t, trimAfter.String(), l0root)
}

func TestInteropEdgeCasesInsertion(t *testing.T) {

	bs := memBs()
	ctx := context.Background()
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
	insertionMst := cidMapToMst(t, bs, mapToCidMapDecode(t, insertionMap))
	insertionBefore, err := insertionMst.GetPointer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, insertionMst.layer, 1)
	assert.Equal(t, insertionBefore.String(), l1root)

	// insert F, which will push E out of the node with G+H to a new node under D
	insertionMst, err = insertionMst.Add(ctx, "com.example.record/3jqfcqzm3fx2j", cid1, -1) // F; level 2
	if err != nil {
		t.Fatal(err)
	}
	insertionAfter, err := insertionMst.GetPointer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, insertionMst.layer, 2)
	assert.Equal(t, insertionAfter.String(), l2root)

	// remove F, which should push E back over with G+H
	insertionMst, err = insertionMst.Delete(ctx, "com.example.record/3jqfcqzm3fx2j") // F; level 2
	if err != nil {
		t.Fatal(err)
	}
	insertionFinal, err := insertionMst.GetPointer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, insertionMst.layer, 1)
	assert.Equal(t, insertionFinal.String(), l1root)
}

// "handles new layers that are two higher than existing"
func TestInteropEdgeCasesHigher(t *testing.T) {

	bs := memBs()
	ctx := context.Background()
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
	higherMst := cidMapToMst(t, bs, mapToCidMapDecode(t, higherMap))
	higherBefore, err := higherMst.GetPointer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, higherMst.layer, 0)
	assert.Equal(t, higherBefore.String(), l0root)

	// insert B, which is two levels above
	higherMst, err = higherMst.Add(ctx, "com.example.record/3jqfcqzm3fx2j", cid1, -1) // B; level 2
	if err != nil {
		t.Fatal(err)
	}
	higherAfter, err := higherMst.GetPointer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, higherAfter.String(), l2root)

	// remove B
	higherMst, err = higherMst.Delete(ctx, "com.example.record/3jqfcqzm3fx2j") // B; level 2
	if err != nil {
		t.Fatal(err)
	}
	higherAgain, err := higherMst.GetPointer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, higherMst.layer, 0)
	assert.Equal(t, higherAgain.String(), l0root)

	// insert B (level=2) and D (level=1)
	higherMst, err = higherMst.Add(ctx, "com.example.record/3jqfcqzm3fx2j", cid1, -1) // B; level 2
	if err != nil {
		t.Fatal(err)
	}
	higherMst, err = higherMst.Add(ctx, "com.example.record/3jqfcqzm4fd2j", cid1, -1) // D; level 1
	if err != nil {
		t.Fatal(err)
	}
	higherYetAgain, err := higherMst.GetPointer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, higherMst.layer, 2)
	assert.Equal(t, higherYetAgain.String(), l2root2)

	// remove D
	higherMst, err = higherMst.Delete(ctx, "com.example.record/3jqfcqzm4fd2j") // D; level 1
	if err != nil {
		t.Fatal(err)
	}
	higherFinal, err := higherMst.GetPointer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, higherMst.layer, 2)
	assert.Equal(t, higherFinal.String(), l2root)
}
