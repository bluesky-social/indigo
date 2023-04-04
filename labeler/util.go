package labeler

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/whyrusleeping/go-did"
)

// TODO:(bnewbold): duplicates elsewhere; should refactor into cliutil
func LoadOrCreateKeyFile(kfile, kid string) (*did.PrivKey, error) {
	_, err := os.Stat(kfile)
	if errors.Is(err, os.ErrNotExist) {
		// file doesn't exist; create a new key and write it out, then we will re-read it
		err = CreateKeyFile(kfile, kid)
		if err != nil {
			return nil, err
		}
	}

	kb, err := os.ReadFile(kfile)
	if err != nil {
		return nil, err
	}

	return ParseSecretKey(string(kb))
}

func CreateKeyFile(kfile, kid string) error {

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

	key.Set(jwk.KeyIDKey, kid)

	buf, err := json.MarshalIndent(key, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal key into JSON: %w", err)
	}

	// ensure data directory exists; won't error if it does
	os.MkdirAll(filepath.Dir(kfile), os.ModePerm)

	return os.WriteFile(kfile, buf, 0664)
}

func ParseSecretKey(val string) (*did.PrivKey, error) {

	sk, err := jwk.ParseKey([]byte(val))
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

	var out string
	kts := string(curve.(jwa.EllipticCurveAlgorithm))
	switch kts {
	case "P-256":
		out = did.KeyTypeP256
	default:
		return nil, fmt.Errorf("unrecognized key type: %s", kts)
	}

	return &did.PrivKey{
		Raw:  &spk,
		Type: out,
	}, nil
}

func dedupeStrings(in []string) []string {
	var out []string
	seen := make(map[string]bool)
	for _, v := range in {
		if !seen[v] {
			out = append(out, v)
			seen[v] = true
		}
	}
	return out
}
