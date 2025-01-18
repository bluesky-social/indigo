package mst

import (
	"crypto/sha256"
)

// Computes the MST "height" for a key (bytestring). Layers are counted from the "bottom" of the tree, starting with zero.
//
// For repo v3, uses SHA-256 as the hashing function and counts two bits at a time, for an MST "fanout" value of 16.
func HeightForKey(key []byte) (height int) {
	hv := sha256.Sum256(key)
	for _, b := range hv {
		if b&0xC0 != 0 {
			// Common case. No leading pair of zero bits.
			break
		}
		if b == 0x00 {
			height += 4
			continue
		}
		if b&0xFC == 0x00 {
			height += 3
		} else if b&0xF0 == 0x00 {
			height += 2
		} else {
			height += 1
		}
		break
	}
	return height
}

func CountPrefixLen(a, b []byte) int {
	// This pattern avoids panicindex calls, as the Go compiler's prove pass can convince itself that neither a[i] nor b[i] are ever out of bounds.
	var i int
	for i = 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return i
}

func IsValidKey(key []byte) bool {
	// TODO: this is mixing repo specification checks and MST checks? Maybe this should be moved to repo-level code?
	if len(key) == 0 || len(key) > 256 {
		return false
	}
	for i := 0; i < len(key); i++ {
		b := key[i]
		if 'a' <= b && b <= 'z' ||
			'A' <= b && b <= 'Z' ||
			'0' <= b && b <= '9' {
			continue
		}
		switch b {
		case '_', ':', '.', '-', '/':
			continue
		default:
			return false
		}
	}
	return true
}
