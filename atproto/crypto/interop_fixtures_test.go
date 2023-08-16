package crypto

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/assert"
)

type InteropFixture struct {
	MessageBase64      string `json:"messageBase64"`
	Algorithm          string `json:"algorithm"`
	DIDDocSuite        string `json:"didDocSuite"`
	PublicKeyDID       string `json:"publicKeyDid"`
	PublicKeyMultibase string `json:"publicKeyMultibase"`
	SignatureBase64    string `json:"signatureBase64"`
	ValidSignature     bool   `json:"validSignature"`
}

func TestInteropSignatureFixtures(t *testing.T) {
	// "p256" == "secp256r1" == "ES256"  == "EcdsaSecp256r1VerificationKey2019"
	// "k256" == "secp256k1" == "ES256K" == "EcdsaSecp256k1VerificationKey2019"

	f, err := os.Open("testdata/signature-fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	fixBytes, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}

	var fixtures []InteropFixture
	if err := json.Unmarshal(fixBytes, &fixtures); err != nil {
		t.Fatal(err)
	}

	for _, row := range fixtures {
		_ = row
		testSignatureFixture(t, row)
	}
}

func testSignatureFixture(t *testing.T, row InteropFixture) {
	assert := assert.New(t)

	// parse all the fields
	pkDID, err := ParsePublicDIDKey(row.PublicKeyDID)
	assert.NoError(err)
	keyBytes, err := base58.Decode(row.PublicKeyMultibase[1:])
	assert.NoError(err)
	msgBytes, err := base64.RawStdEncoding.DecodeString(row.MessageBase64)
	assert.NoError(err)
	sigBytes, err := base64.RawStdEncoding.DecodeString(row.SignatureBase64)
	assert.NoError(err)

	var pkCompMultibase PublicKey
	switch row.DIDDocSuite {
	case "EcdsaSecp256r1VerificationKey2019":
		pkCompMultibase, err = ParsePublicBytesP256(keyBytes)
		assert.NoError(err)
	case "EcdsaSecp256k1VerificationKey2019":
		pkCompMultibase, err = ParsePublicBytesK256(keyBytes)
		assert.NoError(err)
	default:
		t.Fatal("expected DIDDocSuite")
	}

	// verify encodings
	assert.Equal(pkDID, pkCompMultibase, "key equality")
	assert.Equal(row.PublicKeyDID, pkDID.DIDKey(), "did:key re-encoding")

	// verify signatures
	if row.ValidSignature {
		assert.NoError(pkDID.HashAndVerify(msgBytes, sigBytes), "keyType=%v format=%v", row.Algorithm, "did:key")
		assert.NoError(pkCompMultibase.HashAndVerify(msgBytes, sigBytes), "keyType=%v format=%v", row.Algorithm, "multibase")
	} else {
		assert.Error(pkDID.HashAndVerify(msgBytes, sigBytes), "keyType=%v format=%v", row.Algorithm, "did:key")
		assert.Error(pkCompMultibase.HashAndVerify(msgBytes, sigBytes), "keyType=%v format=%v", row.Algorithm, "multibase")
	}

	// signatures don't match random data
	assert.Error(pkCompMultibase.HashAndVerify(msgBytes, []byte{1, 2, 3}), "keyType=%v format=%v", row.Algorithm, "multibase")
	assert.Error(pkCompMultibase.HashAndVerify([]byte{1, 2, 3}, sigBytes), "keyType=%v format=%v", row.Algorithm, "multibase")
}
