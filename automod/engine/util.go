package engine

import (
	"net/url"
	"strings"
)

func dedupeStrings(in []string) []string {
	var out []string
	seen := make(map[string]bool)
	for _, v := range in {
		if !seen[v] {
			out = append(out, v)
			seen[v] = true
		}
	}
	return out
}

// get the cid from a bluesky cdn url
func cidFromCdnUrl(str *string) *string {
	if str == nil {
		return nil
	}

	u, err := url.Parse(*str)
	if err != nil || u.Host != "cdn.bsky.app" {
		return nil
	}

	parts := strings.Split(u.Path, "/")
	if len(parts) != 6 {
		return nil
	}

	return &strings.Split(parts[5], "@")[0]
}
