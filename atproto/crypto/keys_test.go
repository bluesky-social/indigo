package crypto

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInteropSignatures(t *testing.T) {
	assert := assert.New(t)

	testTable := []struct {
		description  string
		keyType      string
		docType      string
		didKey       string
		multibaseKey string
		msgBase64    string
		sigBase64    string
	}{
		{
			description:  "p256/secp256r1",
			keyType:      "ES256",
			docType:      "EcdsaSecp256r1VerificationKey2019",
			msgBase64:    "oWVoZWxsb2V3b3JsZA",
			didKey:       "did:key:zDnaembgSGUhZULN2Caob4HLJPaxBh92N7rtH21TErzqf8HQo",
			multibaseKey: "zxdM8dSstjrpZaRUwBmDvjGXweKuEMVN95A9oJBFjkWMh",
			sigBase64:    "2vZNsG3UKvvO/CDlrdvyZRISOFylinBh0Jupc6KcWoJWExHptCfduPleDbG3rko3YZnn9Lw0IjpixVmexJDegg",
		},
		{
			description:  "k256/secp256k1",
			keyType:      "ES256K",
			docType:      "EcdsaSecp256k1VerificationKey2019",
			didKey:       "did:key:zQ3shueETAVp1HCdE7pcrUD1wEHNhfaQiDJmWeJgCxZRmDJuy",
			multibaseKey: "zRvvEWNhRDxCYpkWWKzvrCP5Sca54yhgXjLcSgfJsWW9Jk8Wq79d5h2PTW3BQDmNQv4prqGjRkXiEDwnfpq2tTsmp",
			msgBase64:    "oWVoZWxsb2V3b3JsZA",
			sigBase64:    "FbKI9u/VoY9SFHvtsuYsZULkt3QiNRrPUP3N4bX0xTxfWIUccyoNJ7egTITlaD1xBhXAvDdBnbyC6aOZ0jyjEA",
		},
	}

	for _, row := range testTable {
		pkDid, err := ParsePublicDidKey(row.didKey)
		assert.NoError(err)
		pkMultibase, err := ParsePublicMultibase(row.multibaseKey, row.docType)
		assert.NoError(err)
		msgBytes, err := base64.RawStdEncoding.DecodeString(row.msgBase64)
		assert.NoError(err)
		sigBytes, err := base64.RawStdEncoding.DecodeString(row.sigBase64)
		assert.NoError(err)

		assert.NoError(pkDid.VerifyBytes(msgBytes, sigBytes), "keyType=%v format=%v", row.description, "did:key")
		assert.NoError(pkMultibase.VerifyBytes(msgBytes, sigBytes), "keyType=%v format=%v", row.description, "multibase")
		assert.Error(pkMultibase.VerifyBytes(msgBytes, []byte{1, 2, 3}), "keyType=%v format=%v", row.description, "multibase")
		assert.Error(pkMultibase.VerifyBytes([]byte{1, 2, 3}, sigBytes), "keyType=%v format=%v", row.description, "multibase")

		// TODO: investigate these additional tests, which partially fail, instead of "continue"
		/*
			assert.Equal(pkDid, pkMultibase, row.description)
			assert.NotEqual(pkDid.MultibaseString(), "<invalid key>", "keyType=%v format=%v", row.description, "did:key")
			assert.NotEqual(pkMultibase.MultibaseString(), "<invalid key>", "keyType=%v format=%v", row.description, "multibase")

			// check that keys round-trip ok
			assert.Equal(row.didKey, pkDid.DID(), "export keyType=%v format=%v", row.description, "did:key")
			assert.Equal(row.multibaseKey, pkDid.MultibaseString(), "export keyType=%v format=%v", row.description, "multibase")
			pkDid = parseDidKey(t, pkDid.DID())
			pkMultibase = parseKeyFromMultibase(t, pkMultibase.MultibaseString(), row.docType)

			assert.NoError(pkDid.Verify(msgBytes, sigBytes), "round-trip keyType=%v format=%v", row.description, "did:key")
			assert.NoError(pkMultibase.Verify(msgBytes, sigBytes), "round-trip keyType=%v format=%v", row.description, "multibase")
		*/
	}
}
