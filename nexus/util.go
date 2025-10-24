package main

import (
	"errors"
	"math/rand"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/xrpc"
)

func backoff(retries int, max int) time.Duration {
	dur := 1 << retries
	if dur > max {
		dur = max
	}

	jitter := time.Millisecond * time.Duration(rand.Intn(1000))
	return time.Second*time.Duration(dur) + jitter
}

// matchesCollection checks if a collection matches any of the provided filters.
// Filters support wildcards at the end (e.g., "app.bsky.*" & "app.bsky.feed.*" both match "app.bsky.feed.post").
// If no filters are provided, all collections match.
func matchesCollection(collection string, filters []string) bool {
	if len(filters) == 0 {
		return true
	}

	for _, filter := range filters {
		if strings.HasSuffix(filter, "*") {
			prefix := strings.TrimSuffix(filter, "*")
			if strings.HasPrefix(collection, prefix) {
				return true
			}
		} else {
			if collection == filter {
				return true
			}
		}
	}

	return false
}

func isRateLimitError(err error) bool {
	var xrpcErr *xrpc.Error
	if errors.As(err, &xrpcErr) {
		return xrpcErr.IsThrottled()
	}
	return false
}
