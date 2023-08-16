package crypto

import (
	"fmt"
	"strings"

	"github.com/mr-tron/base58"
)

// Common interface for private keys of all the supported cryptographic systems in the atproto ecosystem, in a format which may or may not have secret key material directly available in memory to be exported as bytes.
type PrivateKey interface {
	Equal(other PrivateKey) bool

	// Returns the public key for this private key. Verifies that the public
	// key is valid and will be possible to encode as bytes or a string later.
	Public() (PublicKey, error)

	// First hashes the raw bytes, then signs the digest, returning a binary
	// signature. SHA-256 is the hash algorithm used, as specified by atproto.
	// This method always returns a "low-S" signature, as required by atproto.
	HashAndSign(content []byte) ([]byte, error)
}

// Common interface for private keys of all the supported cryptographic systems in the atproto ecosystem, in a format which does have secret key material directly available in memory to be exported as bytes.
type PrivateKeyExportable interface {
	Equal(other PrivateKey) bool

	// Outputs an untyped (no multicodec) compact encoding of the secret key
	// material. The encoding format is curve-specific, and is generally
	// "compact" for private keys. Both P-256 and K-256 private keys end up 32
	// bytes long. There is no ASN.1 or other enclosing structure to the binary
	// encoding.
	Bytes() []byte

	// Same as PrivateKey.Public()
	Public() (PublicKey, error)

	// Same as PrivateKey.HashAndSign()
	HashAndSign(content []byte) ([]byte, error)
}

// Common interface for public keys of all the supported cryptographic systems in the atproto ecosystem.
type PublicKey interface {
	Equal(other PublicKey) bool

	// Outputs a compact byte serialization of this key.
	Bytes() []byte

	// Hashes content bytes with SHA-256, then verifies the signature of the
	// digest.
	HashAndVerify(content, sig []byte) error

	// String serialization of the public key using common parameters:
	// compressed byte serialization; multicode varint code prefix; base58btc
	// string encoding ("z" prefix)
	Multibase() string

	// String serialization of the public key as did:key.
	DidKey() string

	// Outputs a non-compact byte serialization of this key. This is not used
	// frequently, or directly in atproto, but some serializations and
	// encodings require it.
	// For curves with no compressed/uncompressed distinction, returns the same
	// value as Bytes().
	UncompressedBytes() []byte
}

// Parses a public key from multibase encoding, with multicodec indicating the key type.
func ParsePublicMultibase(encoded string) (PublicKey, error) {
	if len(encoded) < 2 || encoded[0] != 'z' {
		return nil, fmt.Errorf("crypto: not a multibase base58btc string")
	}
	data, err := base58.Decode(encoded[1:])
	if err != nil {
		return nil, fmt.Errorf("crypto: not a multibase base58btc string")
	}
	if len(data) < 3 {
		return nil, fmt.Errorf("crypto: multibase key was too short")
	}
	if data[0] == 0x80 && data[1] == 0x24 {
		// multicodec p256-pub, code 0x1200, varint-encoded bytes: [0x80, 0x24]
		return ParsePublicBytesP256(data[2:])
	} else if data[0] == 0xE7 && data[1] == 0x01 {
		// multicodec secp256k1-pub, code 0xE7, varint bytes: [0xE7, 0x01]
		return ParsePublicBytesK256(data[2:])
	} else {
		return nil, fmt.Errorf("unsupported atproto key type (unknown multicodec prefix)")
	}
}

// Loads a [PublicKey] from did:key string serialization.
//
// The did:key format encodes the key type.
func ParsePublicDidKey(didKey string) (PublicKey, error) {
	if !strings.HasPrefix(didKey, "did:key:z") {
		return nil, fmt.Errorf("string is not a DID key: %s", didKey)
	}
	mb := strings.TrimPrefix(didKey, "did:key:")
	return ParsePublicMultibase(mb)
}
