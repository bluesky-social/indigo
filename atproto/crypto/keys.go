package crypto

import (
	"crypto"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"fmt"
	"math/big"
	"strings"

	"github.com/mr-tron/base58"
	secp256k1 "gitlab.com/yawning/secp256k1-voi"
	secp256k1secec "gitlab.com/yawning/secp256k1-voi/secec"
)

type KeyType uint8

const (
	// TODO: consider making these the multiformat table values (public key version); uint16?
	K256 KeyType = 1
	P256 KeyType = 2
)

type PrivateKey struct {
	keyType KeyType
	// XXX: enumerate instead of "any"
	inner any
}

type PublicKey struct {
	keyType KeyType
	// XXX: enumerate instead of "any"
	inner any
}

var k256Options = &secp256k1secec.ECDSAOptions{
	// Used to *verify* digest, not to re-hash
	Hash: crypto.SHA256,
	// Use `[R | S]` encoding.
	Encoding: secp256k1secec.EncodingCompact,
	// Checking `s <= n/2` to prevent signature mallability is not part of SEC 1, Version 2.0. libsecp256k1 which used to be used by this package, includes the check, so retain behavior compatibility.
	RejectMalleable: true,
}

func GeneratePrivateKey(kt KeyType) (*PrivateKey, error) {
	switch kt {
	case P256:
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("P-256/secp256r1 key generation failed: %w", err)
		}
		return &PrivateKey{keyType: kt, inner: key}, nil
	case K256:
		key, err := secp256k1secec.GenerateKey()
		if err != nil {
			return nil, fmt.Errorf("K-256/secp256k1 key generation failed: %w", err)
		}
		return &PrivateKey{keyType: kt, inner: key}, nil
	default:
		return nil, fmt.Errorf("unexpected crypto KeyType")
	}
}

func ParsePrivateKeyBytes(data []byte, kt KeyType) (*PrivateKey, error) {
	switch kt {
	case P256:
		// elaborately parse as an ecdh.PrivateKey, then get from that to ecdsa.PrivateKey by encoding/decoding using x509 PKCS8 encoding.
		// Note that the 'data' bytes format is *not* x509 PKCS8!
		skEcdh, err := ecdh.P256().NewPrivateKey(data)
		if err != nil {
			// TODO: better error msg
			return nil, fmt.Errorf("invalid p256 private key")
		}
		enc, err := x509.MarshalPKCS8PrivateKey(skEcdh)
		if err != nil {
			// TODO: better error msg
			return nil, fmt.Errorf("invalid p256 private key")
		}
		sk, err := x509.ParsePKCS8PrivateKey(enc)
		if err != nil {
			// TODO: better error msg
			return nil, fmt.Errorf("invalid p256 private key")
		}
		return &PrivateKey{keyType: kt, inner: sk}, nil
	case K256:
		sk, err := secp256k1secec.NewPrivateKey(data)
		if err != nil {
			return nil, fmt.Errorf("invalid K-256/secp256k1 private key: %w", err)
		}
		return &PrivateKey{keyType: kt, inner: sk}, nil
	default:
		return nil, fmt.Errorf("unexpected crypto KeyType")
	}
}

func (k *PrivateKey) Equal(other *PrivateKey) bool {
	if k.keyType != other.keyType {
		return false
	}
	switch k.keyType {
	case P256:
		sk := k.inner.(*ecdsa.PrivateKey)
		skOther := other.inner.(*ecdsa.PrivateKey)
		return sk.Equal(skOther)
	case K256:
		sk := k.inner.(*secp256k1secec.PrivateKey)
		skOther := other.inner.(**secp256k1secec.PrivateKey)
		return sk.Equal(skOther)
	default:
		panic("unexpected crypto KeyType")
	}
}

func (k *PrivateKey) Bytes() ([]byte, error) {
	switch k.keyType {
	case P256:
		sk := k.inner.(*ecdsa.PrivateKey)
		// TODO: replace with ecdh's Bytes()
		//return elliptic.MarshalCompressed(sk.Curve, sk.X, sk.Y), nil
		skEcdh, err := sk.ECDH()
		if err != nil {
			// XXX
			return nil, err
		}
		return skEcdh.Bytes(), nil
	case K256:
		return k.inner.(*secp256k1secec.PrivateKey).Bytes(), nil
	default:
		return nil, fmt.Errorf("unexpected crypto KeyType")
	}
}

func (k *PrivateKey) Public() PublicKey {
	switch k.keyType {
	case P256:
		sk := k.inner.(*ecdsa.PrivateKey)
		return PublicKey{
			keyType: k.keyType,
			inner:   sk.Public().(*ecdsa.PublicKey),
		}
	case K256:
		sk := k.inner.(*secp256k1secec.PrivateKey)
		return PublicKey{
			keyType: k.keyType,
			inner:   sk.PublicKey(),
		}
	default:
		panic("unexpected crypto KeyType")
	}
}

func (k *PrivateKey) HashAndSign(content []byte) ([]byte, error) {
	hash := sha256.Sum256(content)
	switch k.keyType {
	case P256:
		r, s, err := ecdsa.Sign(rand.Reader, k.inner.(*ecdsa.PrivateKey), hash[:])
		if err != nil {
			return nil, fmt.Errorf("crypto error signing with P-256/secp256r1 private key: %w", err)
		}
		s = sigSToLowS_P256(s)
		sig := make([]byte, 64)
		r.FillBytes(sig[:32])
		s.FillBytes(sig[32:])
		return sig, nil
	case K256:
		sk := k.inner.(*secp256k1secec.PrivateKey)
		return sk.Sign(rand.Reader, hash[:], k256Options)
	default:
		return nil, fmt.Errorf("unexpected crypto KeyType")
	}
}

func (k *PublicKey) Equal(other *PublicKey) bool {
	if k.keyType != other.keyType {
		return false
	}
	// XXX: return reflect.DeepEqual(k.UncompressedBytes(), other.UncompressedBytes())
	switch k.keyType {
	case P256:
		pk := k.inner.(*ecdsa.PublicKey)
		pkOther := other.inner.(*ecdsa.PublicKey)
		return pk.Equal(pkOther)
	case K256:
		pk := k.inner.(*secp256k1secec.PublicKey)
		pkOther := other.inner.(*secp256k1secec.PublicKey)
		return pk.Equal(pkOther)
	default:
		panic("unexpected crypto KeyType")
	}
}

func ParsePublicCompressedBytes(data []byte, kt KeyType) (*PublicKey, error) {
	switch kt {
	case P256:
		curve := elliptic.P256()
		x, y := elliptic.UnmarshalCompressed(curve, data)
		if x == nil {
			// TODO: better error msg
			return nil, fmt.Errorf("invalid p256 public key")
		}
		if !curve.Params().IsOnCurve(x, y) {
			// TODO: better error msg
			return nil, fmt.Errorf("invalid p256 public key")
		}
		pub := &ecdsa.PublicKey{
			Curve: curve,
			X:     x,
			Y:     y,
		}
		return &PublicKey{
			keyType: kt,
			inner:   pub,
		}, nil
	case K256:
		// secp256k1secec.NewPublicKey accepts any valid encoding, while we
		// explicitly want compressed, so use the explicit point
		// decompression routine.
		p, err := secp256k1.NewIdentityPoint().SetCompressedBytes(data)
		if err != nil {
			// TODO: better error msg
			return nil, fmt.Errorf("invalid k256 public key: %w", err)
		}

		pub, err := secp256k1secec.NewPublicKeyFromPoint(p)
		if err != nil {
			// TODO: better error msg
			return nil, fmt.Errorf("invalid k256 public key: %w", err)
		}
		return &PublicKey{
			keyType: kt,
			inner:   pub,
		}, nil
	default:
		return nil, fmt.Errorf("unexpected crypto KeyType")
	}
}

// Parses a public key in multibase encoding, as would be found in a DID Document `verificationMethod` section. This does not handle the many possible multibase variations (eg, base32 encoding).
func ParsePublicMultibase(encoded string, kt KeyType) (*PublicKey, error) {
	if len(encoded) < 2 || encoded[0] != 'z' {
		return nil, fmt.Errorf("crypto: not a multibase base58btc string")
	}
	data, err := base58.Decode(encoded[1:])
	if err != nil {
		return nil, fmt.Errorf("crypto: not a multibase base58btc string")
	}
	return ParsePublicCompressedBytes(data, kt)
}

func ParsePublicDidKey(didKey string) (*PublicKey, error) {
	if !strings.HasPrefix(didKey, "did:key:z") {
		return nil, fmt.Errorf("string is not a DID key: %s", didKey)
	}
	mb := strings.TrimPrefix(didKey, "did:key:z")
	data, err := base58.Decode(mb)
	if err != nil || len(data) < 2 {
		return nil, fmt.Errorf("crypto: not a multibase base58btc string")
	}
	switch (uint16(data[0]) << 8) + uint16(data[1]) {
	// multicodec p256-pub, code 0x1200, varint-encoded bytes: [0x80, 0x24]
	case 0x8024:
		return ParsePublicCompressedBytes(data[2:], P256)
	// multicodec secp256k1-pub, code 0xE7, varint bytes: [0xE7, 0x01]
	case 0xE701:
		return ParsePublicCompressedBytes(data[2:], K256)
	default:
		return nil, fmt.Errorf("unexpected did:key multicode value")
	}
}

func (k *PublicKey) UncompressedBytes() []byte {
	switch k.keyType {
	case P256:
		pk := k.inner.(*ecdsa.PublicKey)
		if !pk.Curve.IsOnCurve(pk.X, pk.Y) {
			panic("crypto: bad public key")
		}
		// TODO: replace with ecdh's PublicKey.Bytes()
		return elliptic.Marshal(pk.Curve, pk.X, pk.Y)
	case K256:
		pk := k.inner.(*secp256k1secec.PublicKey)
		p := pk.Point()
		// TODO: is this check necessary for uncompressed bytes?
		if p.IsIdentity() != 0 {
			panic("crypto: bad public key")
		}
		return p.UncompressedBytes()
	default:
		panic("unexpected crypto KeyType")
	}
}

func (k *PublicKey) CompressedBytes() []byte {
	switch k.keyType {
	case P256:
		pk := k.inner.(*ecdsa.PublicKey)
		if !pk.Curve.IsOnCurve(pk.X, pk.Y) {
			panic("crypto: bad public key")
		}
		return elliptic.MarshalCompressed(pk.Curve, pk.X, pk.Y)
	case K256:
		pk := k.inner.(*secp256k1secec.PublicKey)
		p := pk.Point()
		if p.IsIdentity() != 0 {
			panic("crypto: bad public key")
		}
		return p.CompressedBytes()
	default:
		panic("unexpected crypto KeyType")
	}
}

func (k *PublicKey) HashAndVerify(content, sig []byte) error {
	hash := sha256.Sum256(content)
	switch k.keyType {
	case P256:
		pk := k.inner.(*ecdsa.PublicKey)

		// parseP256Sig
		if len(sig) != 64 {
			// XXX: better err msg
			return fmt.Errorf("crypto: P-256 signatures must be 64 bytes")
		}
		r := big.NewInt(0)
		s := big.NewInt(0)
		r.SetBytes(sig[:32])
		s.SetBytes(sig[32:])

		if !ecdsa.Verify(pk, hash[:], r, s) {
			return fmt.Errorf("crypto: invalid signature")
		}

		// ensure that signature is low-S
		if !sigSIsLowS_P256(s) {
			return fmt.Errorf("crypto: invalid signature (high-S P-256)")
		}

		return nil
	case K256:
		pk := k.inner.(*secp256k1secec.PublicKey)
		if !pk.Verify(hash[:], sig, k256Options) {
			return fmt.Errorf("crypto: invalid signature")
		}
		return nil
	default:
		return fmt.Errorf("unexpected crypto KeyType")
	}
}

// Returns a did:key string encoding of the public key, as would be encoded in a DID PLC operation:
// - compressed / compacted binary representation
// - prefix with appropriate curve multicodec bytes
// - encode bytes with base58btc
// - add "z" prefix to indicate encoding
// - add "did:key:" prefix
func (k *PublicKey) DidKey() string {
	kbytes := k.CompressedBytes()
	switch k.keyType {
	case P256:
		// multicodec p256-pub, code 0x1200, varint-encoded bytes: [0x80, 0x24]
		kbytes = append([]byte{0x80, 0x24}, kbytes...)
	case K256:
		// multicodec secp256k1-pub, code 0xE7, varint bytes: [0xE7, 0x01]
		kbytes = append([]byte{0xE7, 0x01}, kbytes...)
	default:
		panic("unexpected crypto KeyType")
	}
	return "did:key:z" + base58.Encode(kbytes)
}

// Returns multibase string encoding of the public key, as would be included in a DID Document "verificationMethod" section:
// - non-compressed / non-compacted binary representation
// - encode bytes with base58btc
// - prefix "z" (lower-case) to indicate encoding
func (k *PublicKey) Multibase() string {
	kbytes := k.UncompressedBytes()
	return "z" + base58.Encode(kbytes)
}

func (k *PublicKey) CompressedMultibase() string {
	kbytes := k.CompressedBytes()
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
