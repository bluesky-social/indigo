package cliutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bluesky-social/indigo/atproto/atcrypto"

	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
)

// Loads a secret key from JWK JSON on disk. Only supports P-256 format.
//
// Deprecated: use the indigo atcrypto package and multikey string serialization instead
func LoadKeyFromFile(fpath string) (*atcrypto.PrivateKeyP256, error) {
	kb, err := os.ReadFile(fpath)
	if err != nil {
		return nil, err
	}

	sk, err := jwk.ParseKey(kb)
	if err != nil {
		return nil, err
	}

	var spk ecdsa.PrivateKey
	if err := sk.Raw(&spk); err != nil {
		return nil, err
	}
	curve, ok := sk.Get("crv")
	if !ok {
		return nil, fmt.Errorf("need a curve set")
	}

	kts := string(curve.(jwa.EllipticCurveAlgorithm))
	if kts != "P-256" {
		return nil, fmt.Errorf("unrecognized key type: %s", kts)
	}

	skECDH, err := spk.ECDH()
	if err != nil {
		return nil, err
	}

	return atcrypto.ParsePrivateBytesP256(skECDH.Bytes())
}

// Generates a P-256 secret key and saves it to disk as JWK.
//
// Deprecated: use the indigo atcrypto package and multikey string serialization instead
func GenerateKeyToFile(fname string) error {
	raw, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("failed to generate new ECDSA private key: %s", err)
	}

	key, err := jwk.FromRaw(raw)
	if err != nil {
		return fmt.Errorf("failed to create ECDSA key: %s", err)
	}

	if _, ok := key.(jwk.ECDSAPrivateKey); !ok {
		return fmt.Errorf("expected jwk.ECDSAPrivateKey, got %T", key)
	}

	key.Set(jwk.KeyIDKey, "mykey")

	buf, err := json.MarshalIndent(key, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal key into JSON: %w", err)
	}

	// ensure data directory exists; won't error if it does
	os.MkdirAll(filepath.Dir(fname), os.ModePerm)

	return os.WriteFile(fname, buf, 0664)
}
