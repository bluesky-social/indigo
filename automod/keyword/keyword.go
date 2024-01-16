package keyword

func TokenInSet(tok string, set []string) bool {
	for _, v := range set {
		if tok == v {
			return true
		}
	}
	return false
}
