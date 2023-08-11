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

		// public key encoding
		pub := priv.Public()

		pubCompBytes := pub.CompressedBytes()
		pubFromCompBytes, err := ParsePublicCompressedBytes(pubCompBytes, kt)
		assert.NoError(err)
		assert.True(pub.Equal(pubFromCompBytes))

		pubUncompBytes := pub.UncompressedBytes()
		pubFromUncompBytes, err := ParsePublicUncompressedBytes(pubUncompBytes, kt)
		assert.NoError(err)
		assert.True(pub.Equal(pubFromUncompBytes))

		pubDidKeyString := pub.DidKey()
		pubDK, err := ParsePublicDidKey(pubDidKeyString)
		assert.NoError(err)
		assert.True(pub.Equal(pubDK))

		pubMultibaseString := pub.Multibase()
		pubMB, err := ParsePublicMultibase(pubMultibaseString, kt)
		assert.NoError(err)
		assert.True(pub.Equal(pubMB))

		pubCompMultibaseString := pub.CompressedMultibase()
		pubCMB, err := ParsePublicCompressedMultibase(pubCompMultibaseString, kt)
		assert.NoError(err)
		assert.True(pub.Equal(pubCMB))

		// signature verification
		sig, err := priv.HashAndSign(msg)
		assert.NoError(err)
		assert.NoError(pub.HashAndVerify(msg, sig))

		midSig, err := priv.HashAndSign(midMsg)
		assert.NoError(err)
		assert.NoError(pub.HashAndVerify(midMsg, midSig))

		bigSig, err := priv.HashAndSign(bigMsg)
		assert.NoError(err)
		assert.NoError(pub.HashAndVerify(bigMsg, bigSig))
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
			pub := priv.Public()

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
	pub := priv.Public()
	sig, err := priv.HashAndSign([]byte("test-message"))
	assert.NoError(err)

	// P-256 key and signature sizes
	assert.Equal(32, len(privBytes))
	assert.Equal(33, len(pub.CompressedBytes()))
	assert.Equal(65, len(pub.UncompressedBytes()))
	assert.Equal(64, len(sig))
}

func TestKeyCompressionK256(t *testing.T) {
	assert := assert.New(t)

	priv, err := GeneratePrivateKey(K256)
	assert.NoError(err)
	privBytes, err := priv.Bytes()
	assert.NoError(err)
	pub := priv.Public()
	sig, err := priv.HashAndSign([]byte("test-message"))
	assert.NoError(err)

	// K-256 key and signature sizes
	assert.Equal(32, len(privBytes))
	assert.Equal(33, len(pub.CompressedBytes()))
	assert.Equal(65, len(pub.UncompressedBytes()))
	assert.Equal(64, len(sig))
}
