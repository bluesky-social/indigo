package atcrypto

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseJWK(t *testing.T) {
	assert := assert.New(t)

	jwkTestFixtures := []string{
		// https://datatracker.ietf.org/doc/html/rfc7517#appendix-A.1
		`{
			"kty":"EC",
          	"crv":"P-256",
          	"x":"MKBCTNIcKUSDii11ySs3526iDZ8AiTo7Tu6KPAqv7D4",
          	"y":"4Etl6SRW2YiLUrN5vfvVHuhp7x8PxltmWWlbbM4IFyM",
          	"d":"870MB6gfuTJ4HtUnUvYMyJpr5eUZNP4Bk43bVdj3eAE",
          	"use":"enc",
          	"kid":"1"
      	}`,
		// https://openid.net/specs/draft-jones-json-web-key-03.html; with kty in addition to alg
		`{
			"alg":"EC",
			"kty":"EC",
			"crv":"P-256",
			"x":"MKBCTNIcKUSDii11ySs3526iDZ8AiTo7Tu6KPAqv7D4",
			"y":"4Etl6SRW2YiLUrN5vfvVHuhp7x8PxltmWWlbbM4IFyM",
			"use":"enc",
			"kid":"1"
		}`,
		// https://w3c-ccg.github.io/lds-ecdsa-secp256k1-2019/; with kty in addition to alg
		`{
    		"alg": "EC",
    		"kty": "EC",
    		"crv": "secp256k1",
    		"kid": "JUvpllMEYUZ2joO59UNui_XYDqxVqiFLLAJ8klWuPBw",
    		"x": "dWCvM4fTdeM0KmloF57zxtBPXTOythHPMm1HCLrdd3A",
    		"y": "36uMVGM7hnw-N6GnjFcihWE3SkrhMLzzLCdPMXPEXlA"
  		}`,
	}

	for _, jwkBytes := range jwkTestFixtures {
		_, err := ParsePublicJWKBytes([]byte(jwkBytes))
		assert.NoError(err)
	}
}

func TestP256GenJWK(t *testing.T) {
	assert := assert.New(t)

	priv, err := GeneratePrivateKeyP256()
	if err != nil {
		t.Fatal(err)
	}
	pub, err := priv.PublicKey()
	if err != nil {
		t.Fatal(err)
	}

	pk, ok := pub.(*PublicKeyP256)
	if !ok {
		t.Fatal()
	}
	jwk, err := pk.JWK()
	if err != nil {
		t.Fatal(err)
	}

	_, err = ParsePublicJWK(*jwk)
	assert.NoError(err)
}

func TestK256GenJWK(t *testing.T) {
	assert := assert.New(t)

	priv, err := GeneratePrivateKeyK256()
	if err != nil {
		t.Fatal(err)
	}
	pub, err := priv.PublicKey()
	if err != nil {
		t.Fatal(err)
	}

	pk, ok := pub.(*PublicKeyK256)
	if !ok {
		t.Fatal()
	}
	jwk, err := pk.JWK()
	if err != nil {
		t.Fatal(err)
	}

	_, err = ParsePublicJWK(*jwk)
	assert.NoError(err)
}
