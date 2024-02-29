package lexicon

import (
	"strings"
)

// checks if val matches pattern, with optional trailing glob on pattern. case-sensitive.
func acceptableMimeType(pattern, val string) bool {
	if val == "" || pattern == "" {
		return false
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := pattern[:len(pattern)-1]
		return strings.HasPrefix(val, prefix)
	} else {
		return pattern == val
	}
}
