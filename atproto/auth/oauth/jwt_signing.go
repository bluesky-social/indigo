package oauth

import (
	"crypto"
	"fmt"

	atcrypto "github.com/bluesky-social/indigo/atproto/crypto"
	"github.com/golang-jwt/jwt/v5"
)

// TODO: this is just copied from indigo:atproto/auth/jwt_signing.go
// Need to decide whether to make this a public interface or what... a bit worried about import loops. Maybe part of crypto package?

var (
	signingMethodES256K *signingMethodAtproto
	signingMethodES256  *signingMethodAtproto
	supportedAlgs       []string
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

	signingMethodES256K = &signingMethodAtproto{
		alg:      "ES256K",
		hash:     crypto.SHA256,
		toOutSig: toES256K,
		sigLen:   64,
	}
	jwt.RegisterSigningMethod(signingMethodES256K.Alg(), func() jwt.SigningMethod {
		return signingMethodES256K
	})
	signingMethodES256 = &signingMethodAtproto{
		alg:      "ES256",
		hash:     crypto.SHA256,
		toOutSig: toES256,
		sigLen:   64,
	}
	jwt.RegisterSigningMethod(signingMethodES256.Alg(), func() jwt.SigningMethod {
		return signingMethodES256
	})
	supportedAlgs = []string{signingMethodES256K.Alg(), signingMethodES256.Alg()}
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

	// NOTE: important to use using "lenient" variant here
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

func toES256K(sig []byte) []byte {
	return sig[:64]
}

func toES256(sig []byte) []byte {
	return sig[:64]
}

func keySigningMethod(key atcrypto.PrivateKey) (jwt.SigningMethod, error) {
	switch key.(type) {
	case *atcrypto.PrivateKeyP256:
		return signingMethodES256, nil
	case *atcrypto.PrivateKeyK256:
		return signingMethodES256K, nil
	}
	return nil, fmt.Errorf("unknown key type: %T", key)
}
