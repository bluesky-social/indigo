package oauth

import (
	"crypto"
	"fmt"

	atcrypto "github.com/bluesky-social/indigo/atproto/crypto"
	"github.com/golang-jwt/jwt/v5"
)

// NOTE: this file is copied from indigo:atproto/auth/jwt_signing.go, with the K-256 (ES256) support removed

var (
	signingMethodES256 *signingMethodAtproto
	supportedAlgs      []string
)

// Implementation of jwt.SigningMethod for the `atproto/crypto` types.
type signingMethodAtproto struct {
	alg      string
	hash     crypto.Hash
	toOutSig toOutSig
	sigLen   int
}

type toOutSig func(sig []byte) []byte

func init() {
	// tells JWT library to serialize 'aud' as regular string, not array of strings (when signing)
	jwt.MarshalSingleStringAsArray = false

	signingMethodES256 = &signingMethodAtproto{
		alg:      "ES256",
		hash:     crypto.SHA256,
		toOutSig: toES256,
		sigLen:   64,
	}
	jwt.RegisterSigningMethod(signingMethodES256.Alg(), func() jwt.SigningMethod {
		return signingMethodES256
	})
	supportedAlgs = []string{signingMethodES256.Alg()}
}

func (sm *signingMethodAtproto) Verify(signingString string, sig []byte, key interface{}) error {
	pub, ok := key.(atcrypto.PublicKey)
	if !ok {
		return jwt.ErrInvalidKeyType
	}

	if !sm.hash.Available() {
		return jwt.ErrHashUnavailable
	}

	if len(sig) != sm.sigLen {
		return jwt.ErrTokenSignatureInvalid
	}

	// NOTE: important to use using "lenient" variant here. atproto cryptography is strict about details like low-S elliptic curve signatures, but OAuth cryptography is not, and we want to be interoperable with general purpose OAuth implementations
	return pub.HashAndVerifyLenient([]byte(signingString), sig)
}

func (sm *signingMethodAtproto) Sign(signingString string, key interface{}) ([]byte, error) {
	priv, ok := key.(atcrypto.PrivateKey)
	if !ok {
		return nil, jwt.ErrInvalidKeyType
	}

	return priv.HashAndSign([]byte(signingString))
}

func (sm *signingMethodAtproto) Alg() string {
	return sm.alg
}

func toES256(sig []byte) []byte {
	return sig[:64]
}

func keySigningMethod(key atcrypto.PrivateKey) (jwt.SigningMethod, error) {
	switch key.(type) {
	case *atcrypto.PrivateKeyP256:
		return signingMethodES256, nil
	case *atcrypto.PrivateKeyK256:
		return nil, fmt.Errorf("only P-256 (ES256) private keys supported for atproto OAuth")
	}
	return nil, fmt.Errorf("unknown key type: %T", key)
}
