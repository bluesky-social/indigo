package identity

import (
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDIDDocParse(t *testing.T) {
	assert := assert.New(t)
	docFiles := []string{
		"testdata/did_plc_doc.json",
		"testdata/did_plc_doc_legacy.json",
	}
	for _, path := range docFiles {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()

		docBytes, err := io.ReadAll(f)
		if err != nil {
			t.Fatal(err)
		}

		var doc DIDDocument
		err = json.Unmarshal(docBytes, &doc)
		assert.NoError(err)

		id := ParseIdentity(&doc)

		assert.Equal("did:plc:ewvi7nxzyoun6zhxrhs64oiz", id.DID.String())
		assert.Equal([]string{"at://atproto.com"}, id.AlsoKnownAs)
		pk, err := id.PublicKey()
		assert.NoError(err)
		assert.NotNil(pk)
		assert.Equal("https://bsky.social", id.PDSEndpoint())
		hdl, err := id.DeclaredHandle()
		assert.NoError(err)
		assert.Equal("atproto.com", hdl.String())

		// NOTE: doesn't work if 'id' was in long form
		if path != "testdata/did_plc_doc_legacy.json" {
			assert.Equal(doc, id.DIDDocument())
		}
	}
}

func TestDIDDocFeedGenParse(t *testing.T) {
	assert := assert.New(t)
	f, err := os.Open("testdata/did_web_doc.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	docBytes, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}

	var doc DIDDocument
	err = json.Unmarshal(docBytes, &doc)
	assert.NoError(err)

	id := ParseIdentity(&doc)

	assert.Equal("did:web:discover.bsky.social", id.DID.String())
	assert.Equal([]string{}, id.AlsoKnownAs)
	pk, err := id.PublicKey()
	assert.Error(err)
	assert.ErrorIs(err, ErrKeyNotDeclared)
	assert.Nil(pk)
	assert.Equal("", id.PDSEndpoint())
	hdl, err := id.DeclaredHandle()
	assert.Error(err)
	assert.Empty(hdl)
	svc, ok := id.Services["bsky_fg"]
	assert.True(ok)
	assert.Equal("https://discover.bsky.social", svc.URL)
}

func TestDIDDocWithServiceAuthKey(t *testing.T) {
	assert := assert.New(t)
	f, err := os.Open("testdata/did_plc_doc_service_key.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	docBytes, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}

	var doc DIDDocument
	err = json.Unmarshal(docBytes, &doc)
	assert.NoError(err)

	id := ParseIdentity(&doc)

	assert.Equal("did:plc:ewvi7nxzyoun6zhxrhs64oiz", id.DID.String())

	// PublicKey() should return the #atproto key
	pk, err := id.PublicKey()
	assert.NoError(err)
	assert.NotNil(pk)

	// ServiceAuthPublicKey() should return the #atproto_service key when present
	svcPk, err := id.ServiceAuthPublicKey()
	assert.NoError(err)
	assert.NotNil(svcPk)

	// The two keys should be different
	assert.NotEqual(pk.Multibase(), svcPk.Multibase())

	// Verify the keys are what we expect
	assert.Equal("zQ3shXjHeiBuRCKmM36cuYnm7YEMzhGnCmCyW92sRJ9pribSF", pk.Multibase())
	assert.Equal("zQ3shnCVbvDo2HV5ER5nVqJkxPTW1ShYcRJP6qWWj7tP1LG44", svcPk.Multibase())
}

func TestServiceAuthPublicKeyFallback(t *testing.T) {
	assert := assert.New(t)

	// Test fallback to #atproto when #atproto_service is not present
	f, err := os.Open("testdata/did_plc_doc.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	docBytes, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}

	var doc DIDDocument
	err = json.Unmarshal(docBytes, &doc)
	assert.NoError(err)

	id := ParseIdentity(&doc)

	// Both PublicKey() and ServiceAuthPublicKey() should return the same key
	pk, err := id.PublicKey()
	assert.NoError(err)
	assert.NotNil(pk)

	svcPk, err := id.ServiceAuthPublicKey()
	assert.NoError(err)
	assert.NotNil(svcPk)

	// Should be the same key (fallback behavior)
	assert.Equal(pk.Multibase(), svcPk.Multibase())
}
