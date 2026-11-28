/*
LtHash multiset implementation with BLAKE3, as specified by atproto spaces.

Uses BLAKE3 expanded output mode (XOF) and 1024 little-endian uint16 lanes.

# Acknowledgements

LtHash was originally described in https://eprint.iacr.org/2019/227, using a slightly different hash function and parameters.

This implementation was heavily influenced by reading https://github.com/lukechampine/lthash, though it is not a verbatim copy.
*/
package lthash
