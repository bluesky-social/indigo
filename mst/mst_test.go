package mst

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"maps"
	"math/rand"
	"os"
	"regexp"
	"sort"
	"testing"

	"github.com/bluesky-social/indigo/util"
	cid "github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	"github.com/ipld/go-car/v2"
	"github.com/multiformats/go-multihash"
	mh "github.com/multiformats/go-multihash"
)

func randCid() cid.Cid {
	buf := make([]byte, 32)
	rand.Read(buf)
	c, err := cid.NewPrefixV1(cid.Raw, mh.SHA2_256).Sum(buf)
	if err != nil {
		panic(err)
	}
	return c
}

func TestBasicMst(t *testing.T) {
	rand.Seed(6123123)

	ctx := context.Background()
	cst := util.CborStore(blockstore.NewBlockstore(datastore.NewMapDatastore()))
	mst := createMST(cst, cid.Undef, []nodeEntry{}, -1)

	vals := map[string]cid.Cid{
		"cats/cats":  randCid(),
		"dogs/dogs":  randCid(),
		"cats/bears": randCid(),
	}

	for k, v := range vals {
		nmst, err := mst.Add(ctx, k, v, -1)
		if err != nil {
			t.Fatal(err)
		}
		mst = nmst
	}

	ncid, err := mst.GetPointer(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if ncid.String() != "bafyreidap7hdugsxisef7esd2eh26423j23r65mvlvpsdv7vbbsl5qfgxq" {
		t.Fatal("mst generation changed", ncid.String())
	}

	// delete a key
	nmst, err := mst.Delete(ctx, "dogs/dogs")
	if err != nil {
		t.Fatal(err)
	}
	delete(vals, "dogs/dogs")
	assertValues(t, nmst, vals)

	// update a key
	newCid := randCid()
	vals["cats/cats"] = newCid
	nmst, err = nmst.Update(ctx, "cats/cats", newCid)
	if err != nil {
		t.Fatal(err)
	}
	assertValues(t, nmst, vals)

	// update deleted key should fail
	_, err = nmst.Delete(ctx, "dogs/dogs")
	if err == nil {
		t.Fatal("can't delete a removed key")
	}
	_, err = nmst.Update(ctx, "dogs/dogs", newCid)
	if err == nil {
		t.Fatal("can't update a removed key")
	}
}

func assertValues(t *testing.T, mst *MerkleSearchTree, vals map[string]cid.Cid) {
	out := make(map[string]cid.Cid)
	if err := mst.WalkLeavesFrom(context.TODO(), "", func(key string, val cid.Cid) error {
		out[key] = val
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if len(vals) == len(out) {
		for k, v := range vals {
			ov, ok := out[k]
			if !ok {
				t.Fatalf("expected key %s to be present", k)
			}
			if ov != v {
				t.Fatalf("value mismatch on %s", k)
			}
		}
	} else {
		t.Fatalf("different number of values than expected: %d != %d", len(vals), len(out))
	}
}

func mustCid(t *testing.T, s string) cid.Cid {
	t.Helper()
	c, err := cid.Decode(s)
	if err != nil {
		t.Fatal(err)
	}
	return c

}

func loadCar(bs blockstore.Blockstore, fname string) error {
	fi, err := os.Open(fname)
	if err != nil {
		return err
	}
	defer fi.Close()
	br, err := car.NewBlockReader(fi)
	if err != nil {
		return err
	}

	for {
		blk, err := br.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		if err := bs.Put(context.TODO(), blk); err != nil {
			return err
		}
	}

	return nil
}

/*
func TestDiff(t *testing.T) {
	to := mustCid(t, "bafyreie5cvv4h45feadgeuwhbcutmh6t2ceseocckahdoe6uat64zmz454")
	from := mustCid(t, "bafyreigv5er7vcxlbikkwedmtd7b3kp7wrcyffep5ogcuxosloxfox5reu")

	bs := blockstore.NewBlockstore(datastore.NewMapDatastore())

	if err := loadCar(bs, "paul.car"); err != nil {
		t.Fatal(err)
	}

	ctx := context.TODO()
	ops, err := DiffTrees(ctx, bs, from, to)
	if err != nil {
		t.Fatal(err)
	}
	_ = ops
}
*/

func randStr(s int64) string {
	buf := make([]byte, 6)
	r := rand.New(rand.NewSource(s))
	r.Read(buf)
	return hex.EncodeToString(buf)
}

func TestDiffInsertionsBasic(t *testing.T) {
	a := map[string]string{
		"cats/asdf":     randStr(1),
		"cats/foosesdf": randStr(2),
	}

	b := maps.Clone(a)
	b["cats/bawda"] = randStr(3)
	b["cats/crosasd"] = randStr(4)

	testMapDiffs(t, a, b)
	testMapDiffs(t, b, a)
}

func randKey(s int64) string {
	r := rand.New(rand.NewSource(s))

	top := r.Int63n(6)
	mid := r.Int63n(3)

	end := randStr(r.Int63n(10000000))

	return randStr(125125+top) + "." + randStr(858392+mid) + "/" + end
}

func TestDiffInsertionsLarge(t *testing.T) {
	a := map[string]string{}
	for i := int64(0); i < 1000; i++ {
		a[randKey(i)] = randStr(72385739 - i)
	}

	b := maps.Clone(a)
	for i := int64(0); i < 30; i++ {
		b[randKey(5000+i)] = randStr(2293825 - i)
	}

	testMapDiffs(t, a, b)
	testMapDiffs(t, b, a)
}

func TestDiffNoOverlap(t *testing.T) {
	a := map[string]string{}
	for i := int64(0); i < 10; i++ {
		a[randKey(i)] = randStr(72385739 - i)
	}

	b := map[string]string{}
	for i := int64(0); i < 10; i++ {
		b[randKey(5000+i)] = randStr(2293825 - i)
	}

	testMapDiffs(t, a, b)
	testMapDiffs(t, b, a)
}

func TestDiffSmallOverlap(t *testing.T) {
	a := map[string]string{}
	for i := int64(0); i < 10; i++ {
		a[randKey(i)] = randStr(72385739 - i)
	}

	b := maps.Clone(a)

	for i := int64(0); i < 1000; i++ {
		a[randKey(i)] = randStr(682823 - i)
	}

	for i := int64(0); i < 1000; i++ {
		b[randKey(5000+i)] = randStr(2293825 - i)
	}

	testMapDiffs(t, a, b)
	//testMapDiffs(t, b, a)
}

func TestDiffSmallOverlapSmall(t *testing.T) {
	a := map[string]string{}
	for i := int64(0); i < 4; i++ {
		a[randKey(i)] = randStr(72385739 - i)
	}

	b := maps.Clone(a)

	for i := int64(0); i < 20; i++ {
		a[randKey(i)] = randStr(682823 - i)
	}

	for i := int64(0); i < 20; i++ {
		b[randKey(5000+i)] = randStr(2293825 - i)
	}

	testMapDiffs(t, a, b)
	//testMapDiffs(t, b, a)
}

func TestDiffMutationsBasic(t *testing.T) {
	a := map[string]string{
		"cats/asdf":     randStr(1),
		"cats/foosesdf": randStr(2),
	}

	b := maps.Clone(a)
	b["cats/asdf"] = randStr(3)

	testMapDiffs(t, a, b)
}

func diffMaps(a, b map[string]cid.Cid) []*DiffOp {
	var akeys, bkeys []string

	for k := range a {
		akeys = append(akeys, k)
	}

	for k := range b {
		bkeys = append(bkeys, k)
	}

	sort.Strings(akeys)
	sort.Strings(bkeys)

	var out []*DiffOp
	for _, k := range akeys {
		av := a[k]
		bv, ok := b[k]
		if !ok {
			out = append(out, &DiffOp{
				Op:     "del",
				Rpath:  k,
				OldCid: av,
			})
		} else {
			if av != bv {
				out = append(out, &DiffOp{
					Op:     "mut",
					Rpath:  k,
					OldCid: av,
					NewCid: bv,
				})
			}
		}
	}

	for _, k := range bkeys {
		_, ok := a[k]
		if !ok {
			out = append(out, &DiffOp{
				Op:     "add",
				Rpath:  k,
				NewCid: b[k],
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Rpath < out[j].Rpath
	})

	return out
}

// NOTE(bnewbold): this does *not* just call cid.Decode(), which is the simple
// "parse a CID in string form into a cid.Cid object". This method (strToCid())
// can sometimes result in "identity" CIDs
func strToCid(s string) cid.Cid {
	h, err := multihash.Sum([]byte(s), multihash.ID, -1)
	if err != nil {
		panic(err)
	}

	return cid.NewCidV1(cid.Raw, h)

}

func mapToCidMap(a map[string]string) map[string]cid.Cid {
	out := make(map[string]cid.Cid)
	for k, v := range a {
		out[k] = strToCid(v)
	}

	return out
}

func cidMapToMst(t testing.TB, bs blockstore.Blockstore, m map[string]cid.Cid) *MerkleSearchTree {
	cst := util.CborStore(bs)
	mt := createMST(cst, cid.Undef, []nodeEntry{}, -1)

	for k, v := range m {
		nmst, err := mt.Add(context.TODO(), k, v, -1)
		if err != nil {
			t.Fatal(err)
		}

		mt = nmst
	}

	return mt
}

func mustCidTree(t testing.TB, tree *MerkleSearchTree) cid.Cid {
	c, err := tree.GetPointer(context.TODO())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func memBs() blockstore.Blockstore {
	return blockstore.NewBlockstore(datastore.NewMapDatastore())
}

func testMapDiffs(t testing.TB, a, b map[string]string) {
	amc := mapToCidMap(a)
	bmc := mapToCidMap(b)

	exp := diffMaps(amc, bmc)

	bs := memBs()

	msta := cidMapToMst(t, bs, amc)
	mstb := cidMapToMst(t, bs, bmc)

	cida := mustCidTree(t, msta)
	cidb := mustCidTree(t, mstb)

	diffs, err := DiffTrees(context.TODO(), bs, cida, cidb)
	if err != nil {
		t.Fatal(err)
	}

	if !sort.SliceIsSorted(diffs, func(i, j int) bool {
		return diffs[i].Rpath < diffs[j].Rpath
	}) {
		t.Log("diff algo did not produce properly sorted diff")
	}
	if !compareDiffs(diffs, exp) {
		fmt.Println("Expected Diff:")
		for _, do := range exp {
			fmt.Println(do)
		}
		fmt.Println("Actual Diff:")
		for _, do := range diffs {
			fmt.Println(do)
		}
		t.Logf("diff lens: %d %d", len(diffs), len(exp))
		diffDiff(diffs, exp)
		t.Fatal("diffs not equal")
	}
}

func diffDiff(a, b []*DiffOp) {
	var i, j int

	for i < len(a) || j < len(b) {
		if i >= len(a) {
			fmt.Println("+: ", b[j])
			j++
			continue
		}

		if j >= len(b) {
			fmt.Println("-: ", a[i])
			i++
			continue
		}

		aa := a[i]
		bb := b[j]

		if diffOpEq(aa, bb) {
			fmt.Println("eq: ", i, j, aa.Rpath)
			i++
			j++
			continue
		}

		if aa.Rpath == bb.Rpath {
			fmt.Println("~: ", aa, bb)
			i++
			j++
			continue
		}

		if aa.Rpath < bb.Rpath {
			fmt.Println("-: ", aa)
			i++
			continue
		} else {
			fmt.Println("+: ", bb)
			j++
			continue
		}
	}
}

func compareDiffs(a, b []*DiffOp) bool {
	if len(a) != len(b) {
		return false
	}

	for i := 0; i < len(a); i++ {
		aa := a[i]
		bb := b[i]

		if aa.Op != bb.Op || aa.Rpath != bb.Rpath || aa.NewCid != bb.NewCid || aa.OldCid != bb.OldCid {
			return false
		}
	}

	return true
}

func diffOpEq(aa, bb *DiffOp) bool {
	if aa.Op != bb.Op || aa.Rpath != bb.Rpath || aa.NewCid != bb.NewCid || aa.OldCid != bb.OldCid {
		return false
	}
	return true
}

func BenchmarkIsValidMstKey(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if !isValidMstKey("foo/foo.bar123") {
			b.Fatal()
		}
	}
}

func TestLeadingZerosOnHashAllocs(t *testing.T) {
	var sink int
	const in = "some.key.prefix/key.bar123456789012334556"
	var inb = []byte(in)
	if n := int(testing.AllocsPerRun(1000, func() {
		sink = leadingZerosOnHash(in)
	})); n != 0 {
		t.Errorf("allocs (string) = %d; want 0", n)
	}
	if n := int(testing.AllocsPerRun(1000, func() {
		sink = leadingZerosOnHashBytes(inb)
	})); n != 0 {
		t.Errorf("allocs (bytes) = %d; want 0", n)
	}
	_ = sink
}

// Verify that keyHasAllValidChars matches its documented regexp.
func FuzzKeyHasAllValidChars(f *testing.F) {
	for _, seed := range [][]byte{{}} {
		f.Add(seed)
	}
	for i := 0; i < 256; i++ {
		f.Add([]byte{byte(i)})
	}
	rx := regexp.MustCompile("^[a-zA-Z0-9_:.-]+$")
	f.Fuzz(func(t *testing.T, in []byte) {
		s := string(in)
		if a, b := rx.MatchString(s), keyHasAllValidChars(s); a != b {
			t.Fatalf("for %q, rx=%v, keyHasAllValidChars=%v", s, a, b)
		}
	})
}

func BenchmarkLeadingZerosOnHash(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = leadingZerosOnHash("some.key.prefix/key.bar123456789012334556")
	}
}

func BenchmarkDiffTrees(b *testing.B) {
	b.ReportAllocs()
	const size = 10000
	ma := map[string]string{}
	for i := 0; i < size; i++ {
		ma[fmt.Sprintf("num/%02d", i)] = fmt.Sprint(i)
	}
	// And then mess with half of the items of the first half of it.
	mb := maps.Clone(ma)
	for i := 0; i < size/2; i++ {
		switch i % 4 {
		case 0, 1:
		case 2:
			delete(mb, fmt.Sprintf("num/%02d", i))
		case 3:
			ma[fmt.Sprintf("num/%02d", i)] = fmt.Sprint(i + 1)
		}
	}

	amc := mapToCidMap(ma)
	bmc := mapToCidMap(mb)

	want := diffMaps(amc, bmc)

	bs := memBs()

	msta := cidMapToMst(b, bs, amc)
	mstb := cidMapToMst(b, bs, bmc)

	cida := mustCidTree(b, msta)
	cidb := mustCidTree(b, mstb)

	b.ResetTimer()

	var diffs []*DiffOp
	var err error
	for i := 0; i < b.N; i++ {
		diffs, err = DiffTrees(context.TODO(), bs, cida, cidb)
		if err != nil {
			b.Fatal(err)
		}
	}

	if !sort.SliceIsSorted(diffs, func(i, j int) bool {
		return diffs[i].Rpath < diffs[j].Rpath
	}) {
		b.Log("diff algo did not produce properly sorted diff")
	}
	if !compareDiffs(diffs, want) {
		b.Fatal("diffs not equal")
	}
}

var countPrefixLenTests = []struct {
	a, b string
	want int
}{
	{"", "", 0},
	{"a", "", 0},
	{"", "a", 0},
	{"a", "b", 0},
	{"a", "a", 1},
	{"ab", "a", 1},
	{"a", "ab", 1},
	{"ab", "ab", 2},
	{"abcdefghijklmnop", "abcdefghijklmnoq", 15},
}

func TestCountPrefixLen(t *testing.T) {
	for _, tt := range countPrefixLenTests {
		if got := countPrefixLen(tt.a, tt.b); got != tt.want {
			t.Errorf("countPrefixLenTests(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func BenchmarkCountPrefixLen(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for _, tt := range countPrefixLenTests {
			if got := countPrefixLen(tt.a, tt.b); got != tt.want {
				b.Fatalf("countPrefixLenTests(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		}
	}
}
