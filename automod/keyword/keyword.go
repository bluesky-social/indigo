package keyword

// Helper to check a single token against a list of tokens
func TokenInSet(tok string, set []string) bool {
	for _, v := range set {
		if tok == v {
			return true
		}
	}
	return false
}
