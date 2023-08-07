package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	//"crypto/sha256"
	//"io"
	"fmt"

	//secp256k1 "gitlab.com/yawning/secp256k1-voi"
	"github.com/mr-tron/base58"
	secp256k1secec "gitlab.com/yawning/secp256k1-voi/secec"
)

type KeyType uint8

const (
	K256 KeyType = 1
	P256 KeyType = 2
)

type PrivateKey struct {
	keyType KeyType
	value   any
}

type PublicKey struct {
	keyType KeyType
	value   any
}

// Parses a public key in multibase encoding, as would be found in a DID Document `verificationMethod` section. This does not handle the many possible multibase variations.
func ParsePublicMultibase(encoded, didDocSuite string) (*PublicKey, error) {
	return nil, fmt.Errorf("NOT IMPLEMENTED")
}

func ParsePublicDidKey(didKey string) (*PublicKey, error) {
	return nil, fmt.Errorf("NOT IMPLEMENTED")
}

func GeneratePrivateKey(kt KeyType) (*PrivateKey, error) {
	switch kt {
	case P256:
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("P-256/secp256r1 key generation failed: %w", err)
		}
		return &PrivateKey{keyType: kt, value: key}, nil
	case K256:
		key, err := secp256k1secec.GenerateKey()
		if err != nil {
			return nil, fmt.Errorf("K-256/secp256k1 key generation failed: %w", err)
		}
		return &PrivateKey{keyType: kt, value: key}, nil
	default:
		return nil, fmt.Errorf("unknown key type")
	}
}

func (k *PrivateKey) SignBytes(content []byte) []byte {
	panic("NOT IMPLEMENTED")
}

func (k *PrivateKey) HashAndSign(content []byte) []byte {
	panic("NOT IMPLEMENTED")
}

func (k *PrivateKey) VerifyBytes(content []byte) bool {
	panic("NOT IMPLEMENTED")
}

func (k *PrivateKey) HashAndVerify(content []byte) bool {
	panic("NOT IMPLEMENTED")
}

func (k *PrivateKey) Bytes() []byte {
	panic("NOT IMPLEMENTED")
}

func (k *PrivateKey) PublicKey() PublicKey {
	panic("NOT IMPLEMENTED")
}

func (k *PublicKey) UncompressedBytes() []byte {
	panic("NOT IMPLEMENTED")
}

func (k *PublicKey) CompressedBytes() []byte {
	panic("NOT IMPLEMENTED")
}

// Returns a did:key string encoding of the public key, as would be encoded in a DID PLC operation:
// - compressed / compacted binary representation
// - prefix with appropriate curve multicodec bytes
// - encode bytes with base58btc
// - add "did:key:" prefix
func (k *PublicKey) DidKey() string {
	kbytes := k.CompressedBytes()
	switch k.keyType {
	case P256:
		// TODO: multicodec p256-pub, code 0x1200, varint-encoded bytes: [0x80, 0x24]
		kbytes = append([]byte{0x80, 0x24}, kbytes...)
	case K256:
		// TODO: multicodec secp256k1-pub, code 0xE7, varint bytes: [0xE7, 0x01]
		kbytes = append([]byte{0xE7, 0x01}, kbytes...)
	default:
		panic("unexpected crypto KeyType")
	}
	return "did:key:" + base58.Encode(kbytes)
}

// Returns multibase string encoding of the public key, as would be included in a DID Document "verificationMethod" section:
// - non-compressed / non-compacted binary representation
// - encode bytes with base58btc
// - prefix "z" (lower-case) to indicate encoding
func (k *PublicKey) Multibase() string {
	kbytes := k.UncompressedBytes()
	return "z" + base58.Encode(kbytes)
}

func (k *PublicKey) DidDocSuite() string {
	switch k.keyType {
	case P256:
		return "EcdsaSecp256r1VerificationKey2019"
	case K256:
		// NOTE: this is not a W3C standard suite, and will probably be replaced with "Multikey"
		return "EcdsaSecp256k1VerificationKey2019"
	default:
		panic("unexpected crypto KeyType")
	}
}
