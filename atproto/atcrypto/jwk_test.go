package atcrypto

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func testGenJWK(t *testing.T, priv PrivateKey, jwk JWK) {
	assert := assert.New(t)
	require := require.New(t)
	val := []byte("dummy")

	sig, err := priv.HashAndSign(val)
	require.NoError(err)

	parsed, err := ParsePublicJWK(jwk)
	require.NoError(err)

	assert.NoError(parsed.HashAndVerify(val, sig))
}

func TestP256GenJWK(t *testing.T) {
	require := require.New(t)

	priv, err := GeneratePrivateKeyP256()
	require.NoError(err)
	pub, err := priv.PublicKey()
	require.NoError(err)
	pk, ok := pub.(*PublicKeyP256)
	require.True(ok)
	jwk, err := pk.JWK()
	require.NoError(err)

	testGenJWK(t, priv, *jwk)
}

func TestK256GenJWK(t *testing.T) {
	require := require.New(t)

	priv, err := GeneratePrivateKeyK256()
	require.NoError(err)
	pub, err := priv.PublicKey()
	require.NoError(err)
	pk, ok := pub.(*PublicKeyK256)
	require.True(ok)
	jwk, err := pk.JWK()
	require.NoError(err)

	testGenJWK(t, priv, *jwk)
}

func TestZeroPaddedGenJWK(t *testing.T) {
	assert := require.New(t)
	require := require.New(t)

	/* Code snippet used to generate this test case
	for range 10000 {
		priv, err := GeneratePrivateKeyP256()
		require.NoError(err)
		pub, err := priv.PublicKey()
		require.NoError(err)
		pk, ok := pub.(*PublicKeyP256)
		require.True(ok)

		if len(pk.pubP256.X.Bytes()) != 32 {
			fmt.Println(priv.Multibase())
		}
		//require.Len(pk.pubP256.X.Bytes(), 32)
	}
	*/

	// P-256 key known to have a leading zero byte public key X coordinate
	priv, err := ParsePrivateMultibase("z42tuopZtUKiGERFp9i9fSZBvRxbbCddi9TBaQVzYx635y5m")
	require.NoError(err)

	pub, err := priv.PublicKey()
	require.NoError(err)
	pk, ok := pub.(*PublicKeyP256)
	require.True(ok)
	jwk, err := pk.JWK()
	require.NoError(err)

	assert.Len(pk.pubP256.X.Bytes(), 31)

	_, err = ParsePublicJWK(*jwk)
	require.NoError(err)

	testGenJWK(t, priv, *jwk)
}
