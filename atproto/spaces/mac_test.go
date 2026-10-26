package spaces

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSharedSecretIndependentP256Vector(t *testing.T) {
	// Constructed outside Go with independent affine P-256 arithmetic and
	// HMAC-SHA256 Extract/Expand, using syncer scalar 3 and author scalar 7.
	decode := func(value string) []byte {
		t.Helper()
		data, err := hex.DecodeString(value)
		require.NoError(t, err)
		return data
	}
	dh := decode("3250fcf686637c7b2e4ac86eb473bca53a582139f42b1523fd76364e67399e83")
	syncer := decode("045ecbe4d1a6330a44c8f7ef951d4bf165e6c6b721efada985fb41661bc6e7fd6c8734640c4998ff7e374b06ce1a64a2ecd82ab036384fb83d9a79b127a27d5032")
	author := decode("048e533b6fa0bf7b4625bb30667c01fb607ef9f8b8a80fef5b300628703187b2a373eb1dbde03318366d069f83a6f5900053c73633cb041b21c55e1a86c1f400b4")
	secret, err := deriveSharedSecret(dh, syncer, author)
	require.NoError(t, err)
	require.Equal(t, "23ebcdbb09613e45efeeb6030b912ca08b6d38c1825f3db60ac830aed5f0c4e9", hex.EncodeToString(secret[:]))
	// Swapping the public keys must not authenticate the same exchange.
	reversed, err := deriveSharedSecret(dh, author, syncer)
	require.NoError(t, err)
	require.NotEqual(t, secret, reversed)
}
