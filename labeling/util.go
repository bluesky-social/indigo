package labeling

import (
	"crypto/ecdsa"
	"fmt"
	"os"

	"github.com/lestrrat-go/jwx/jwa"
	jwk "github.com/lestrrat-go/jwx/jwk"
	"github.com/whyrusleeping/go-did"
)

// TODO:(bnewbold): duplicates elsewhere; should refactor into cliutil
func loadKey(kfile string) (*did.PrivKey, error) {
	kb, err := os.ReadFile(kfile)
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
