package pds

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/lestrrat-go/jwx/v2/jwk"
)

// make a root signing key for testing
func TestJWK_GenKey(t *testing.T) {
	// to get the same values every time, we need to create a static source
	// of "randomness"
	rdr := bytes.NewReader([]byte("01234567890123456789012345678901234567890123456789ABCDEF"))
	raw, err := ecdsa.GenerateKey(elliptic.P384(), rdr)
	if err != nil {
		fmt.Printf("failed to generate new ECDSA private key: %s\n", err)
		return
	}

	key, err := jwk.FromRaw(raw)
	if err != nil {
		fmt.Printf("failed to create ECDSA key: %s\n", err)
		return
	}
	if _, ok := key.(jwk.ECDSAPrivateKey); !ok {
		fmt.Printf("expected jwk.ECDSAPrivateKey, got %T\n", key)
		return
	}

	key.Set(jwk.KeyIDKey, "mykey")

	buf, err := json.MarshalIndent(key, "", "  ")
	if err != nil {
		fmt.Printf("failed to marshal key into JSON: %s\n", err)
		return
	}
	fmt.Printf("%s\n", buf)

	// OUTPUT:
	// {
	//   "crv": "P-384",
	//   "d": "ODkwMTIzNDU2Nzg5MDEyMz7deMbyLt8g4cjcxozuIoygLLlAeoQ1AfM9TSvxkFHJ",
	//   "kid": "mykey",
	//   "kty": "EC",
	//   "x": "gvvRMqm1w5aHn7sVNA2QUJeOVcedUnmiug6VhU834gzS9k87crVwu9dz7uLOdoQl",
	//   "y": "7fVF7b6J_6_g6Wu9RuJw8geWxEi5ja9Gp2TSdELm5u2E-M7IF-bsxqcdOj3n1n7N"
	// }
}
