package repo

import (
	"bytes"
	"context"
	"os"
	"strconv"
	"testing"

	"github.com/bluesky-social/indigo/atproto/repo/mst"

	"github.com/ipfs/go-cid"
)

// TODO: this function isn't actually used
func exampleBigMap(size int) map[string]cid.Cid {
	c, _ := cid.Decode("bafkreieqq463374bbcbeq7gpmet5rvrpeqow6t4rtjzrkhnlu222222222")
	m := make(map[string]cid.Cid, size)
	for i := range size {
		m[strconv.Itoa(i)] = c
	}
	return m
}

func BenchmarkCreateAndHash(b *testing.B) {
	b.ReportAllocs()
	m := exampleBigMap(10_000)

	for b.Loop() {
		tree, err := mst.LoadTreeFromMap(m)
		if err != nil {
			b.Fatal(err)
		}
		_, err = tree.RootCID()
		if err != nil {
			b.Fatal(err)
		}
	}

}

func BenchmarkLoadFromCAR(b *testing.B) {
	b.ReportAllocs()
	ctx := context.Background()

	// NOTE: recommend to use a larger repo
	//carPath := "bnewbold.net.20250310004504.car"
	carPath := "testdata/bnewbold.robocracy.org.20250310005405.car"
	carBytes, err := os.ReadFile(carPath)
	if err != nil {
		b.Fatal(err)
	}

	for b.Loop() {
		buf := bytes.NewBuffer(carBytes)
		_, _, err := LoadFromCAR(ctx, buf)
		if err != nil {
			b.Fatal(err)
		}
	}
}
