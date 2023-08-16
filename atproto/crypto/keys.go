package crypto

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mr-tron/base58"
)

// Common interface for all the supported atproto cryptographic systems, when
// secret key material may not be directly available to be exported as bytes.
type PrivateKey interface {
	Equal(other PrivateKey) bool

	// If necessary, pre-verifies that the public key curve point is valid and
	// will be possible to encode as bytes or a string later.
	Public() (PublicKey, error)

	// Hashes the raw bytes using SHA-256, then signs the digest bytes.
	// Always returns a "low-S" signature (for elliptic curve systems where that is ambigious).
	HashAndSign(content []byte) ([]byte, error)
}

// Common interface for all the supported atproto cryptographic systems, when
// secret key material is directly available to be exported as bytes.
type PrivateKeyExportable interface {
	PrivateKey

	// Untyped (no multicodec) encoding of the secret key material.
	// The encoding format is curve-specific, and is generally "compact" for private keys.
	// No ASN.1 or other enclosing structure is applied to the bytes.
	Bytes() []byte
}

// Common interface for all the supported atproto cryptographic systems.
type PublicKey interface {
	Equal(other PublicKey) bool

	// Compact byte serialization (for elliptic curve systems where encoding is ambigious).
	Bytes() []byte

	// Hashes the raw bytes using SHA-256, then verifies the signature of the digest bytes.
	HashAndVerify(content, sig []byte) error

	// String serialization of the key bytes using common parameters:
	// compressed byte serialization; multicode varint code prefix; base58btc
	// string encoding ("z" prefix)
	Multibase() string

	// String serialization of the key bytes as a did:key.
	DIDKey() string

	// Non-compact byte serialization (for elliptic curve systems where
	// encoding is ambigious)
	//
	// This is not used frequently, or directly in atproto, but some
	// serializations and encodings require it.
	//
	// For systems with no compressed/uncompressed distinction, returns the same
	// value as Bytes().
	UncompressedBytes() []byte
}

var ErrInvalidSignature = errors.New("crytographic signature invalid")

// Loads a public key from multibase string encoding, with multicodec indicating the key type.
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
func ParsePublicDIDKey(didKey string) (PublicKey, error) {
	if !strings.HasPrefix(didKey, "did:key:z") {
		return nil, fmt.Errorf("string is not a DID key: %s", didKey)
	}
	mb := strings.TrimPrefix(didKey, "did:key:")
	return ParsePublicMultibase(mb)
}
