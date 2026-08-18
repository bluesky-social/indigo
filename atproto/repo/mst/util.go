package mst

import (
	"crypto/sha256"
)

const (
	// Maximum length, in bytes, of a key in the tree. Note that the atproto specifications imply a repo path maximum length, but don't say anything directly about MST key, other than they can not be empty (zero-length).
	MAX_KEY_BYTES = 1024
)

// Computes the MST "height" for a key (bytestring). Layers are counted from the "bottom" of the tree, starting with zero.
//
// For atproto repository v3, uses SHA-256 as the hashing function and counts two bits at a time, for an MST "fanout" value of 16.
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

// Computes the common prefix length between two bytestrings.
//
// Used when compacting node entry lists for encoding.
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
	if len(key) == 0 || len(key) > MAX_KEY_BYTES {
		return false
	}
	return true
}
