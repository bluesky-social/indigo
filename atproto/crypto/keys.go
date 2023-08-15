package crypto

import (
	"fmt"
	"strings"

	"github.com/mr-tron/base58"
)

type PrivateKey interface {
	Public() (PublicKey, error)
	HashAndSign(content []byte) ([]byte, error)
}

type PrivateKeyExportable interface {
	Bytes() []byte
	Equal(other PrivateKeyExportable) bool
	Public() (PublicKey, error)
	HashAndSign(content []byte) ([]byte, error)
}

type PublicKey interface {
	UncompressedBytes() []byte
	Bytes() []byte
	Equal(other PublicKey) bool
	HashAndVerify(content, sig []byte) error
	Multibase() string
	DidKey() string

	// these are likely to be deprecated
	LegacyDidDocSuite() string
	LegacyMultibase() string
}

// Parses a public key in multibase encoding, as would be found in a older DID Document `verificationMethod` section.
//
// This implementation does not handle the many possible multibase encodings (eg, base32), only the base58btc encoding that would be found in a DID Document.
//
// This function is deprecated!
func ParsePublicLegacyMultibase(encoded string, didDocSuite string) (PublicKey, error) {
	if len(encoded) < 2 || encoded[0] != 'z' {
		return nil, fmt.Errorf("crypto: not a multibase base58btc string")
	}
	data, err := base58.Decode(encoded[1:])
	if err != nil {
		return nil, fmt.Errorf("crypto: not a multibase base58btc string")
	}
	switch didDocSuite {
	case "EcdsaSecp256r1VerificationKey2019":
		return ParsePublicUncompressedBytesP256(data)
	case "EcdsaSecp256k1VerificationKey2019":
		return ParsePublicUncompressedBytesK256(data)
	default:
		return nil, fmt.Errorf("unhandled legacy crypto suite: %s", didDocSuite)
	}
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
		return nil, fmt.Errorf("unexpected multicode code for multibase-encoded key")
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
