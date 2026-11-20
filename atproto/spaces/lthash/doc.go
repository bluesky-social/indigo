// Package lthash implements the incremental LtHash set hash using BLAKE3 XOF
// expansion, 1024 little-endian uint16 lanes, and a SHA-256 digest of the state.
//
// This implementation was extracted from Cobalt's atspace/lthash package, which
// was influenced by github.com/lukechampine/lthash rather than copied verbatim.
package lthash
