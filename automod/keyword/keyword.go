package keyword

import "slices"

// Helper to check a single token against a list of tokens
func TokenInSet(tok string, set []string) bool {
	return slices.Contains(set, tok)
}
