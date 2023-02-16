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
	assert.Equal(t, leadingZerosOnHash("2653ae71"), 0, msg)
	assert.Equal(t, leadingZerosOnHash("88bfafc7"), 1, msg)
	assert.Equal(t, leadingZerosOnHash("2a92d355"), 2, msg)
	assert.Equal(t, leadingZerosOnHash("884976f5"), 3, msg)
	assert.Equal(t, leadingZerosOnHash("app.bsky.feed.post/454397e440ec"), 2, msg)
	assert.Equal(t, leadingZerosOnHash("app.bsky.feed.post/9adeb165882c"), 4, msg)
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
			TreeEntry{
				PrefixLen: 0,
				KeySuffix: "asdf",
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
	assert.Equal(t, mcid.String(), "bafyreidaftbr35xhh4lzmv5jcoeufqjh75ohzmz6u56v7n2ippbtxdgqqe")

}

func TestInteropKnownMaps(t *testing.T) {

	cid1str := "bafyreie5cvv4h45feadgeuwhbcutmh6t2ceseocckahdoe6uat64zmz454"

	// empty map
	emptyMap := map[string]string{}
	assert.Equal(t, mapToMstRootCidString(t, emptyMap), "bafyreie5737gdxlw5i64vzichcalba3z2v5n6icifvx5xytvske7mr3hpm")

	// no depth, single entry
	trivialMap := map[string]string{
		"asdf": cid1str,
	}
	assert.Equal(t, mapToMstRootCidString(t, trivialMap), "bafyreidaftbr35xhh4lzmv5jcoeufqjh75ohzmz6u56v7n2ippbtxdgqqe")

	// single layer=2 entry
	singlelayer2Map := map[string]string{
		"com.example.record/9ba1c7247ede": cid1str,
	}
	assert.Equal(t, mapToMstRootCidString(t, singlelayer2Map), "bafyreid4g5smj6ukhrjasebt6myj7wmtm2eijouteoyueoqgoh6vm5jkae")

	// pretty simple, but with some depth
	simpleMap := map[string]string{
		"asdf":                            cid1str,
		"88bfafc7":                        cid1str,
		"2a92d355":                        cid1str,
		"app.bsky.feed.post/454397e440ec": cid1str,
		"app.bsky.feed.post/9adeb165882c": cid1str,
	}
	assert.Equal(t, mapToMstRootCidString(t, simpleMap), "bafyreiecb33zh7r2sc3k2wthm6exwzfktof63kmajeildktqc25xj6qzx4")
}

func TestInteropKnownMapsTricky(t *testing.T) {
	t.Skip("TODO: behavior of these wide-char keys is undefined behavior in string MST")

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
func TestInteropEdgeCasesTrim(t *testing.T) {

	bs := memBs()
	ctx := context.Background()
	cid1str := "bafyreie5cvv4h45feadgeuwhbcutmh6t2ceseocckahdoe6uat64zmz454"
	l1root := "bafyreihuyj2vzb2vjw3yhxg6dy25achg5fmre6gg5m6fjtxn64bqju4dee"
	l0root := "bafyreibmijjc63mekkjzl3v2pegngwke5u6cu66g75z6uw27v64bc6ahqi"

	trimMap := map[string]string{
		"com.example.record/40c73105b48f": cid1str, // level 0
		"com.example.record/e99bf3ced34b": cid1str, // level 0
		"com.example.record/893e6c08b450": cid1str, // level 0
		"com.example.record/9cd8b6c0cc02": cid1str, // level 0
		"com.example.record/cbe72d33d12a": cid1str, // level 0
		"com.example.record/a15e33ba0f6c": cid1str, // level 1
	}
	trimMst := cidMapToMst(t, bs, mapToCidMapDecode(t, trimMap))
	trimBefore, err := trimMst.GetPointer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, trimMst.layer, 1)
	assert.Equal(t, trimBefore.String(), l1root)

	trimMst, err = trimMst.Delete(ctx, "com.example.record/a15e33ba0f6c") // level 1
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
	l1root := "bafyreiagt55jzvkenoa4yik77dhomagq2uj26ix4cijj7kd2py2u3s43ve"
	l2root := "bafyreiddrz7qbvfattp5dzzh4ldohsaobatsg7f5l6awxnmuydewq66qoa"
	insertionMap := map[string]string{
		"com.example.record/403e2aeebfdb": cid1str, // A; level 0
		"com.example.record/40c73105b48f": cid1str, // B; level 0
		"com.example.record/645787eb4316": cid1str, // C; level 0
		"com.example.record/7ca4e61d6fbc": cid1str, // D; level 1
		"com.example.record/893e6c08b450": cid1str, // E; level 0
		"com.example.record/9cd8b6c0cc02": cid1str, // G; level 0
		"com.example.record/cbe72d33d12a": cid1str, // H; level 0
		"com.example.record/dbea731be795": cid1str, // I; level 1
		"com.example.record/e2ef555433f2": cid1str, // J; level 0
		"com.example.record/e99bf3ced34b": cid1str, // K; level 0
		"com.example.record/f728ba61e4b6": cid1str, // L; level 0
	}
	insertionMst := cidMapToMst(t, bs, mapToCidMapDecode(t, insertionMap))
	insertionBefore, err := insertionMst.GetPointer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, insertionMst.layer, 1)
	assert.Equal(t, insertionBefore.String(), l1root)

	// insert F, which will push E out of the node with G+H to a new node under D
	insertionMst, err = insertionMst.Add(ctx, "com.example.record/9ba1c7247ede", cid1, -1) // F; level 2
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
	insertionMst, err = insertionMst.Delete(ctx, "com.example.record/9ba1c7247ede") // F; level 2
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

	l0root := "bafyreicivoa3p3ttcebdn2zfkdzenkd2uk3gxxlaz43qvueeip6yysvq2m"
	l2root := "bafyreidwoqm6xlewxzhrx6ytbyhsazctlv72txtmnd4au6t53z2vpzn7wa"
	l2root2 := "bafyreiapru27ce4wdlylk5revtr3hewmxhmt3ek5f2ypioiivmdbv5igrm"
	higherMap := map[string]string{
		"com.example.record/403e2aeebfdb": cid1str, // A; level 0
		"com.example.record/cbe72d33d12a": cid1str, // C; level 0
	}
	higherMst := cidMapToMst(t, bs, mapToCidMapDecode(t, higherMap))
	higherBefore, err := higherMst.GetPointer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, higherMst.layer, 0)
	assert.Equal(t, higherBefore.String(), l0root)

	// insert B, which is two levels above
	higherMst, err = higherMst.Add(ctx, "com.example.record/9ba1c7247ede", cid1, -1) // B; level 2
	if err != nil {
		t.Fatal(err)
	}
	higherAfter, err := higherMst.GetPointer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, higherAfter.String(), l2root)

	// remove B
	higherMst, err = higherMst.Delete(ctx, "com.example.record/9ba1c7247ede") // B; level 2
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
	higherMst, err = higherMst.Add(ctx, "com.example.record/9ba1c7247ede", cid1, -1) // B; level 2
	if err != nil {
		t.Fatal(err)
	}
	higherMst, err = higherMst.Add(ctx, "com.example.record/fae7a851fbeb", cid1, -1) // D; level 1
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
	higherMst, err = higherMst.Delete(ctx, "com.example.record/fae7a851fbeb") // D; level 1
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
