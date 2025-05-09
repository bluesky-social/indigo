package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"

	secp256k1 "gitlab.com/yawning/secp256k1-voi"
	secp256k1secec "gitlab.com/yawning/secp256k1-voi/secec"
)

// Representation of a JSON Web Key (JWK), as relevant to the keys supported by this package.
//
// Expected to be marshalled/unmarshalled as JSON.
type JWK struct {
	KeyType string  `json:"kty"`
	Curve   string  `json:"crv"`
	X       string  `json:"x"` // base64url, no padding
	Y       string  `json:"y"` // base64url, no padding
	Use     string  `json:"use,omitempty"`
	KeyID   *string `json:"kid,omitempty"`
}

// Loads a [PublicKey] from JWK (serialized as JSON bytes)
func ParsePublicJWKBytes(jwkBytes []byte) (PublicKey, error) {
	var jwk JWK
	if err := json.Unmarshal(jwkBytes, &jwk); err != nil {
		return nil, fmt.Errorf("parsing JWK JSON: %w", err)
	}
	return ParsePublicJWK(jwk)
}

// Loads a [PublicKey] from JWK struct.
func ParsePublicJWK(jwk JWK) (PublicKey, error) {

	if jwk.KeyType != "EC" {
		return nil, fmt.Errorf("unsupported JWK key type: %s", jwk.KeyType)
	}

	// base64url with no encoding
	xbuf, err := base64.RawURLEncoding.DecodeString(jwk.X)
	if err != nil {
		return nil, fmt.Errorf("invalid JWK base64 encoding: %w", err)
	}
	ybuf, err := base64.RawURLEncoding.DecodeString(jwk.Y)
	if err != nil {
		return nil, fmt.Errorf("invalid JWK base64 encoding: %w", err)
	}

	switch jwk.Curve {
	case "P-256":
		curve := elliptic.P256()

		var x, y big.Int
		x.SetBytes(xbuf)
		y.SetBytes(ybuf)

		if !curve.Params().IsOnCurve(&x, &y) {
			return nil, fmt.Errorf("invalid P-256 public key (not on curve)")
		}
		pubECDSA := &ecdsa.PublicKey{
			Curve: curve,
			X:     &x,
			Y:     &y,
		}
		pub := PublicKeyP256{pubP256: *pubECDSA}
		err := pub.checkCurve()
		if err != nil {
			return nil, err
		}
		return &pub, nil
	case "secp256k1": // K-256
		if len(xbuf) != 32 || len(ybuf) != 32 {
			return nil, fmt.Errorf("invalid K-256 coordinates")
		}
		xarr := ([32]byte)(xbuf[:32])
		yarr := ([32]byte)(ybuf[:32])
		p, err := secp256k1.NewPointFromCoords(&xarr, &yarr)
		if err != nil {
			return nil, fmt.Errorf("invalid K-256 coordinates: %w", err)
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
	default:
		return nil, fmt.Errorf("unsupported JWK cryptography: %s", jwk.Curve)
	}
}

func (k *PublicKeyP256) JWK() (*JWK, error) {
	jwk := JWK{
		KeyType: "EC",
		Curve:   "P-256",
		X:       base64.RawURLEncoding.EncodeToString(k.pubP256.X.Bytes()),
		Y:       base64.RawURLEncoding.EncodeToString(k.pubP256.Y.Bytes()),
	}
	return &jwk, nil
}

func (k *PublicKeyK256) JWK() (*JWK, error) {
	raw := k.UncompressedBytes()
	if len(raw) != 65 {
		return nil, fmt.Errorf("unexpected K-256 bytes size")
	}
	xbytes := raw[1:33]
	ybytes := raw[33:65]
	jwk := JWK{
		KeyType: "EC",
		Curve:   "secp256k1",
		X:       base64.RawURLEncoding.EncodeToString(xbytes),
		Y:       base64.RawURLEncoding.EncodeToString(ybytes),
	}
	return &jwk, nil
}
