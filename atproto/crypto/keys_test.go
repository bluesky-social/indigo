package crypto

import (
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKeyBasics(t *testing.T) {
	assert := assert.New(t)

	// try signing/verifying a couple different message sizes. these all just get hashed.
	msg := []byte("test-message")
	midMsg := make([]byte, 13*1024)
	_, err := rand.Read(midMsg)
	bigMsg := make([]byte, 16*1024*1024)
	_, err = rand.Read(bigMsg)
	assert.NoError(err)

	// private key generation and byte serialization (P-256)
	privP256, err := GeneratePrivateKeyP256()
	assert.NoError(err)
	privP256Bytes := privP256.Bytes()
	privP256FromBytes, err := ParsePrivateBytesP256(privP256Bytes)
	assert.NoError(err)
	assert.Equal(privP256, privP256FromBytes)

	// private key generation and byte serialization (K-256)
	privK256, err := GeneratePrivateKeyK256()
	assert.NoError(err)
	privK256Bytes := privK256.Bytes()
	privK256FromBytes, err := ParsePrivateBytesK256(privK256Bytes)
	assert.NoError(err)
	assert.Equal(privK256, privK256FromBytes)

	// public key byte serialization (P-256)
	pubP256, err := privP256.Public()
	assert.NoError(err)
	pubP256CompBytes := pubP256.Bytes()
	pubP256FromCompBytes, err := ParsePublicBytesP256(pubP256CompBytes)
	assert.NoError(err)
	assert.True(pubP256.Equal(pubP256FromCompBytes))

	pubP256UncompBytes := pubP256.UncompressedBytes()
	pubP256FromUncompBytes, err := ParsePublicUncompressedBytesP256(pubP256UncompBytes)
	assert.NoError(err)
	assert.True(pubP256.Equal(pubP256FromUncompBytes))

	both := []PrivateKey{privP256, privK256}
	for _, priv := range both {
		pub, err := priv.Public()
		assert.NoError(err)

		// public key encoding
		pubDidKeyString := pub.DidKey()
		pubDK, err := ParsePublicDidKey(pubDidKeyString)
		assert.NoError(err)
		assert.True(pub.Equal(pubDK))

		pubMultibaseString := pub.Multibase()
		pubMB, err := ParsePublicMultibase(pubMultibaseString)
		assert.NoError(err)
		assert.True(pub.Equal(pubMB))

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

	for i := 0; i < 128; i++ {
		privP256, err := GeneratePrivateKeyP256()
		assert.NoError(err)
		privK256, err := GeneratePrivateKeyK256()
		assert.NoError(err)

		both := []PrivateKey{privP256, privK256}
		for _, priv := range both {
			pub, err := priv.Public()
			assert.NoError(err)

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

	priv, err := GeneratePrivateKeyP256()
	assert.NoError(err)
	privBytes := priv.Bytes()
	pub, err := priv.Public()
	assert.NoError(err)
	sig, err := priv.HashAndSign([]byte("test-message"))
	assert.NoError(err)

	// P-256 key and signature sizes
	assert.Equal(32, len(privBytes))
	assert.Equal(33, len(pub.Bytes()))
	assert.Equal(65, len(pub.UncompressedBytes()))
	assert.Equal(64, len(sig))
}

func TestKeyCompressionK256(t *testing.T) {
	assert := assert.New(t)

	priv, err := GeneratePrivateKeyK256()
	assert.NoError(err)
	privBytes := priv.Bytes()
	pub, err := priv.Public()
	assert.NoError(err)
	sig, err := priv.HashAndSign([]byte("test-message"))
	assert.NoError(err)

	// K-256 key and signature sizes
	assert.Equal(32, len(privBytes))
	assert.Equal(33, len(pub.Bytes()))
	assert.Equal(65, len(pub.UncompressedBytes()))
	assert.Equal(64, len(sig))
}
