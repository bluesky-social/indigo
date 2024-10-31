package helpers

import (
	"fmt"
	"regexp"

	"github.com/spaolacci/murmur3"
)

func DedupeStrings(in []string) []string {
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

// returns a fast, compact hash of a string
//
// current implementation uses murmur3, default seed, and hex encoding
func HashOfString(s string) string {
	val := murmur3.Sum64([]byte(s))
	return fmt.Sprintf("%016x", val)
}

// based on: https://stackoverflow.com/a/48769624, with no trailing period allowed
var urlRegex = regexp.MustCompile(`(?:(?:https?|ftp):\/\/)?[\w/\-?=%.]+\.[\w/\-&?=%.]*[\w/\-&?=%]+`)

func ExtractTextURLs(raw string) []string {
	return urlRegex.FindAllString(raw, -1)
}
