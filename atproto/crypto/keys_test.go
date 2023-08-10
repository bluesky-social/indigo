package crypto

import (
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/assert"
)

var keyTypes = []KeyType{K256, P256}

func TestKeyBasics(t *testing.T) {
	assert := assert.New(t)

	// try signing/verifying a couple different message sizes. these all just get hashed.
	msg := []byte("test-message")
	midMsg := make([]byte, 13*1024)
	_, err := rand.Read(midMsg)
	bigMsg := make([]byte, 16*1024*1024)
	_, err = rand.Read(bigMsg)
	assert.NoError(err)

	for _, kt := range keyTypes {
		// private key generation and encoding
		priv, err := GeneratePrivateKey(kt)
		assert.NoError(err)
		privBytes, err := priv.Bytes()
		assert.NoError(err)
		privFromBytes, err := ParsePrivateKeyBytes(privBytes, kt)
		assert.NoError(err)
		assert.Equal(priv, privFromBytes)

		pub := priv.PublicKey()

		sig, err := priv.HashAndSign(msg)
		assert.NoError(err)
		assert.NoError(pub.HashAndVerify(msg, sig))

		midSig, err := priv.HashAndSign(midMsg)
		assert.NoError(err)
		assert.NoError(pub.HashAndVerify(midMsg, midSig))

		bigSig, err := priv.HashAndSign(bigMsg)
		assert.NoError(err)
		assert.NoError(pub.HashAndVerify(bigMsg, bigSig))

		pubDidKeyString := pub.DidKey()
		pubMultibaseString := pub.Multibase()
		pubDK, err := ParsePublicDidKey(pubDidKeyString)
		assert.NoError(err)

		_ = pubMultibaseString
		_ = pubDK
		/* XXX
		arsePublicMultibase(pubMultibaseString, kt)
		assert.NoError(err)
		assert.Equal(pub, pubDK)
		assert.Equal(pub == pubDK, true)
		assert.Equal(pub.Equal(pubDK), true)
		assert.Equal(pub.Equal(pubMB), true)
		*/
	}
}

// this does a large number of sign/verify cycles, to try and hit any bad high-S signatures
func TestLowSMany(t *testing.T) {
	assert := assert.New(t)

	msg := make([]byte, 1024)

	for _, kt := range keyTypes {
		for i := 0; i < 128; i++ {
			priv, err := GeneratePrivateKey(kt)
			assert.NoError(err)
			pub := priv.PublicKey()

			_, err = rand.Read(msg)
			assert.NoError(err)

			sig, err := priv.HashAndSign(msg)
			assert.NoError(err)
			err = pub.HashAndVerify(msg, sig)
			assert.NoError(err)
			// bail out early instead of looping
			if err != nil {
				break
			}
		}
	}
}

func TestKeyCompressionP256(t *testing.T) {
	assert := assert.New(t)

	priv, err := GeneratePrivateKey(P256)
	assert.NoError(err)
	privBytes, err := priv.Bytes()
	assert.NoError(err)
	pub := priv.PublicKey()

	// P-256 key sizes
	// XXX: should be 32 bytes
	assert.Equal(33, len(privBytes))
	assert.Equal(33, len(pub.CompressedBytes()))
	assert.Equal(65, len(pub.UncompressedBytes()))
}

func TestKeyCompressionK256(t *testing.T) {
	assert := assert.New(t)

	priv, err := GeneratePrivateKey(K256)
	assert.NoError(err)
	privBytes, err := priv.Bytes()
	assert.NoError(err)
	pub := priv.PublicKey()

	// K-256 key sizes
	assert.Equal(32, len(privBytes))
	assert.Equal(33, len(pub.CompressedBytes()))
	assert.Equal(65, len(pub.UncompressedBytes()))
}
