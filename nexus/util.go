package main

import "strings"

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
