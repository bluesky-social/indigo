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
