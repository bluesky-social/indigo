package main

import (
	"fmt"
	"strings"
)

// attempts to parse a service URL to an AT-URI, handle, or DID. if it can't, passes string through as-is
func ParseServiceURL(raw string) string {
	parts := strings.Split(raw, "/")
	if len(parts) < 3 || parts[0] != "https:" {
		return raw
	}
	if parts[2] == "bsky.app" && len(parts) >= 5 && parts[3] == "profile" {
		if len(parts) == 5 {
			return parts[4]
		}
		if len(parts) == 7 && parts[5] == "post" {
			return fmt.Sprintf("at://%s/app.bsky.feed.post/%s", parts[4], parts[6])
		}
	}
	return raw
}
