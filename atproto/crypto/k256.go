package crypto

import (
	"crypto"
	"crypto/rand"
	"crypto/sha256"
	"fmt"

	"github.com/mr-tron/base58"
	secp256k1 "gitlab.com/yawning/secp256k1-voi"
	secp256k1secec "gitlab.com/yawning/secp256k1-voi/secec"
)

// Implements the [PrivateKeyExportable] and [PrivateKey] interfaces for the NIST K-256 / secp256k1 / ES256K cryptographic curve.
// Secret key material is naively stored in memory.
type PrivateKeyK256 struct {
	privK256 *secp256k1secec.PrivateKey
}

// K-256 / secp256k1 / ES256K
// Implements the [PublicKey] interface for the NIST K-256 / secp256k1 / ES256K cryptographic curve.
type PublicKeyK256 struct {
	pubK256 *secp256k1secec.PublicKey
}

var _ PrivateKey = (*PrivateKeyK256)(nil)
var _ PrivateKeyExportable = (*PrivateKeyK256)(nil)
var _ PublicKey = (*PublicKeyK256)(nil)

var k256Options = &secp256k1secec.ECDSAOptions{
	// Used to *verify* digest, not to re-hash
	Hash: crypto.SHA256,
	// Use `[R | S]` encoding.
	Encoding: secp256k1secec.EncodingCompact,
	// Checking `s <= n/2` to prevent signature mallability is not part of SEC 1, Version 2.0. libsecp256k1 which used to be used by this package, includes the check, so retain behavior compatibility.
	RejectMalleable: true,
}

var k256LenientOptions = &secp256k1secec.ECDSAOptions{
	// Used to *verify* digest, not to re-hash
	Hash: crypto.SHA256,
	// Use `[R | S]` encoding.
	Encoding: secp256k1secec.EncodingCompact,
	// Allows (eg, for JWT validation)
	RejectMalleable: false,
}

// Creates a secure new cryptographic key from scratch, with the indicated curve type.
func GeneratePrivateKeyK256() (*PrivateKeyK256, error) {
	key, err := secp256k1secec.GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("K-256/secp256k1 key generation failed: %w", err)
	}
	return &PrivateKeyK256{privK256: key}, nil
}

// Loads a [PrivateKeyK256] from raw bytes, as exported by the PrivateKey.Bytes method.
//
// Calling code needs to know the key type ahead of time, and must remove any string encoding (hex encoding, base64, etc) before calling this function.
func ParsePrivateBytesK256(data []byte) (*PrivateKeyK256, error) {
	sk, err := secp256k1secec.NewPrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("invalid K-256/secp256k1 private key: %w", err)
	}
	return &PrivateKeyK256{privK256: sk}, nil
}

// Checks if the two private keys are the same. Note that the naive == operator does not work for most equality checks.
func (k *PrivateKeyK256) Equal(other PrivateKey) bool {
	otherK256, ok := other.(*PrivateKeyK256)
	if ok {
		return k.privK256.Equal(otherK256.privK256)
	}
	return false
}

// Serializes the secret key material in to a raw binary format, which can be parsed by [ParsePrivateBytesK256].
//
// For K-256, this is the "compact" encoding and is 32 bytes long. There is no ASN.1 or other enclosing structure.
func (k PrivateKeyK256) Bytes() []byte {
	return k.privK256.Bytes()
}

// Multibase string encoding of the private key, including a multicodec indicator
func (k *PrivateKeyK256) Multibase() string {
	kbytes := k.Bytes()
	// multicodec secp256k1-priv, code 0x1301, varint-encoded bytes: [0x81, 0x26]
	kbytes = append([]byte{0x81, 0x26}, kbytes...)
	return "z" + base58.Encode(kbytes)
}

// Outputs the [PublicKey] corresponding to this [PrivateKeyK256]; it will be a [PublicKeyK256].
func (k PrivateKeyK256) PublicKey() (PublicKey, error) {
	pub := PublicKeyK256{pubK256: k.privK256.PublicKey()}
	err := pub.ensureBytes()
	if err != nil {
		return nil, err
	}
	return &pub, nil
}

// First hashes the raw bytes, then signs the digest, returning a binary signature.
//
// SHA-256 is the hash algorithm used, as specified by atproto. Signing digests is the norm for ECDSA, and required by some backend implementations. This method does not "double hash", it simply has name which clarifies that hashing is happening.
//
// Calling code is responsible for any string encoding of signatures (eg, hex or base64). For K-256, the signature is 64 bytes long.
//
// NIST ECDSA signatures can have a "malleability" issue, meaning that there are multiple valid signatures for the same content with the same signing key. This method always returns a "low-S" signature, as required by atproto.
func (k PrivateKeyK256) HashAndSign(content []byte) ([]byte, error) {
	hash := sha256.Sum256(content)
	return k.privK256.Sign(rand.Reader, hash[:], k256Options)
}

// Loads a [PublicKeyK256] raw bytes, as exported by the PublicKey.Bytes method. This is the "compressed" curve format.
//
// Calling code needs to know the key type ahead of time, and must remove any string encoding (hex encoding, base64, etc) before calling this function.
func ParsePublicBytesK256(data []byte) (*PublicKeyK256, error) {
	// secp256k1secec.NewPublicKey accepts any valid encoding, while we
	// explicitly want compressed, so use the explicit point
	// decompression routine.
	p, err := secp256k1.NewIdentityPoint().SetCompressedBytes(data)
	if err != nil {
		return nil, fmt.Errorf("invalid K-256/secp256k1 public key: %w", err)
	}

	pubK, err := secp256k1secec.NewPublicKeyFromPoint(p)
	if err != nil {
		return nil, fmt.Errorf("invalid K-256/secp256k1 public key: %w", err)
	}
	pub := PublicKeyK256{pubK256: pubK}
	err = pub.ensureBytes()
	if err != nil {
		return nil, err
	}
	return &pub, nil
}

// Loads a [PublicKeyK256] from raw bytes, as exported by the PublicKey.UncompressedBytes method.
//
// Calling code needs to know the key type ahead of time, and must remove any string encoding (hex encoding, base64, etc) before calling this function.
func ParsePublicUncompressedBytesK256(data []byte) (*PublicKeyK256, error) {
	pubK, err := secp256k1secec.NewPublicKey(data)
	if err != nil {
		return nil, fmt.Errorf("invalid K-256/secp256k1 public key: %w", err)
	}
	pub := PublicKeyK256{pubK256: pubK}
	err = pub.ensureBytes()
	if err != nil {
		return nil, err
	}
	return &pub, nil
}

// Checks if the two public keys are the same. Note that the naive == operator does not work for most equality checks.
func (k *PublicKeyK256) Equal(other PublicKey) bool {
	otherK256, ok := other.(*PublicKeyK256)
	if ok {
		return k.pubK256.Equal(otherK256.pubK256)
	}
	return false
}

// verifies that this public key is safe to export as bytes later on
func (k *PublicKeyK256) ensureBytes() error {
	p := k.pubK256.Point()
	if p.IsIdentity() != 0 {
		return fmt.Errorf("unexpected invalid K-256/secp256k1 public key (internal)")
	}
	return nil
}

// Serializes the key in to "uncompressed" binary format.
func (k *PublicKeyK256) UncompressedBytes() []byte {
	p := k.pubK256.Point()
	return p.UncompressedBytes()
}

// Serializes the key in to "compressed" binary format.
func (k *PublicKeyK256) Bytes() []byte {
	p := k.pubK256.Point()
	return p.CompressedBytes()
}

// First hashes the raw bytes, then verifies the digest, returning `nil` for valid signatures, or an error for any failure.
//
// SHA-256 is the hash algorithm used, as specified by atproto. Signing digests is the norm for ECDSA, and required by some backend implementations. This method does not "double hash", it simply has name which clarifies that hashing is happening.
//
// Calling code is responsible for any string decoding of signatures (eg, hex or base64) before calling this function.
//
// This method requires a "low-S" signature, as specified by atproto.
func (k *PublicKeyK256) HashAndVerify(content, sig []byte) error {
	hash := sha256.Sum256(content)
	if !k.pubK256.Verify(hash[:], sig, k256Options) {
		return ErrInvalidSignature
	}
	return nil
}

// Same as HashAndVerify(), only does not require "low-S" signature.
//
// Used for, eg, JWT validation.
func (k *PublicKeyK256) HashAndVerifyLenient(content, sig []byte) error {
	hash := sha256.Sum256(content)
	if !k.pubK256.Verify(hash[:], sig, k256LenientOptions) {
		return ErrInvalidSignature
	}
	return nil
}

// Returns a multibased string encoding of the public key, including a multicodec indicator and compressed curve bytes serialization
func (k *PublicKeyK256) Multibase() string {
	kbytes := k.Bytes()
	// multicodec secp256k1-pub, code 0xE7, varint bytes: [0xE7, 0x01]
	kbytes = append([]byte{0xE7, 0x01}, kbytes...)
	return "z" + base58.Encode(kbytes)
}

// Returns a did:key string encoding of the public key, as would be encoded in a DID PLC operation:
//
//   - compressed / compacted binary representation
//   - prefix with appropriate curve multicodec bytes
//   - encode bytes with base58btc
//   - add "z" prefix to indicate encoding
//   - add "did:key:" prefix
func (k *PublicKeyK256) DIDKey() string {
	return "did:key:" + k.Multibase()
}
