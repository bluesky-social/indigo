package graphd_test

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/bluesky-social/indigo/graphd"
)

func BenchmarkAcquireDID(b *testing.B) {
	graph := graphd.NewGraph()

	dids := make([]string, b.N)

	// Generate some random DIDs
	for i := 0; i < b.N; i++ {
		dids[i] = fmt.Sprintf("did:example:%d", i)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		graph.AcquireDID(dids[i])
	}
}

func BenchmarkAddFollow(b *testing.B) {
	graph := graphd.NewGraph()

	dids := make([]string, b.N)

	// Generate some random DIDs
	for i := 0; i < b.N; i++ {
		dids[i] = fmt.Sprintf("did:example:%d", i)
	}

	uids := make([]uint64, b.N)

	// Acquire all DIDs
	for i := 0; i < b.N; i++ {
		uids[i] = graph.AcquireDID(dids[i])
	}

	// Create a Zipf distribution with parameters s = 1.07 and v = 1. The
	// value of `s` determines the shape of the distribution. The smaller
	// the value, the more even the distribution; the larger the value,
	// the more skewed the distribution. The value of 1.07 is a common
	// choice, but you can adjust based on your needs.
	zipf := rand.NewZipf(rand.New(rand.NewSource(42)), 1.07, 1, uint64(b.N))

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		followee := uids[zipf.Uint64()%uint64(b.N)] // get a random node according to Zipf's law
		graph.AddFollow(uids[i], followee)
	}
}

func BenchmarkGetFollowers(b *testing.B) {
	graph := graphd.NewGraph()

	dids := make([]string, b.N)

	// Generate some random DIDs
	for i := 0; i < b.N; i++ {
		dids[i] = fmt.Sprintf("did:example:%d", i)
	}

	uids := make([]uint64, b.N)

	// Acquire all DIDs
	for i := 0; i < b.N; i++ {
		uids[i] = graph.AcquireDID(dids[i])
	}

	// Create a Zipf distribution with parameters s = 1.07 and v = 1. The
	// value of `s` determines the shape of the distribution. The smaller
	// the value, the more even the distribution; the larger the value,
	// the more skewed the distribution. The value of 1.07 is a common
	// choice, but you can adjust based on your needs.
	zipf := rand.NewZipf(rand.New(rand.NewSource(42)), 1.07, 1, uint64(b.N))

	for i := 0; i < b.N; i++ {
		followee := uids[zipf.Uint64()%uint64(b.N)] // get a random node according to Zipf's law
		graph.AddFollow(uids[i], followee)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		graph.GetFollowers(uids[i])
	}
}

func BenchmarkDoesFollow(b *testing.B) {
	graph := graphd.NewGraph()

	dids := make([]string, b.N)

	// Generate some random DIDs
	for i := 0; i < b.N; i++ {
		dids[i] = fmt.Sprintf("did:example:%d", i)
	}

	uids := make([]uint64, b.N)

	// Acquire all DIDs
	for i := 0; i < b.N; i++ {
		uids[i] = graph.AcquireDID(dids[i])
	}

	// Create a Zipf distribution with parameters s = 1.07 and v = 1. The
	// value of `s` determines the shape of the distribution. The smaller
	// the value, the more even the distribution; the larger the value,
	// the more skewed the distribution. The value of 1.07 is a common
	// choice, but you can adjust based on your needs.
	zipf := rand.NewZipf(rand.New(rand.NewSource(42)), 1.07, 1, uint64(b.N))

	for i := 0; i < b.N; i++ {
		followee := uids[zipf.Uint64()%uint64(b.N)] // get a random node according to Zipf's law
		graph.AddFollow(uids[i], followee)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		graph.DoesFollow(uids[i], uids[i])
	}
}

func TestIntersectNFollowers(t *testing.T) {
	graph := graphd.NewGraph()

	dids := make([]string, 100)

	// Generate some random DIDs
	for i := 0; i < 100; i++ {
		dids[i] = fmt.Sprintf("did:example:%d", i)
	}

	uids := make([]uint64, 100)

	// Acquire all DIDs
	for i := 0; i < 100; i++ {
		uids[i] = graph.AcquireDID(dids[i])
	}

	// Create a known set of followers
	for i := 0; i < 100; i++ {
		graph.AddFollow(uids[i], uids[0])
	}

	for i := 5; i < 50; i++ {
		graph.AddFollow(uids[i], uids[1])
	}

	for i := 2; i < 25; i++ {
		graph.AddFollow(uids[i], uids[2])
	}

	for i := 0; i < 10; i++ {
		graph.AddFollow(uids[i], uids[3])
	}

	// Intersect the followers of the first 4 DIDs
	intersect, err := graph.IntersectFollowers(uids[:4])
	if err != nil {
		t.Fatal(err)
	}
	if len(intersect) != 5 {
		t.Errorf("expected 5 followers, got %d", len(intersect))
	}

	// Check the followers are the expected ones
	intersectMap := make(map[uint64]bool)
	for _, uid := range intersect {
		intersectMap[uid] = true
	}

	for i := 5; i < 10; i++ {
		if !intersectMap[uids[i]] {
			t.Errorf("expected uid %d to be in the intersection", uids[i])
		}
	}
}
