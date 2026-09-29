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

func TestDIDDocNonStringServiceEndpoint(t *testing.T) {
	assert := assert.New(t)

	// DID Core allows map and set endpoints; those services get dropped, the rest of the doc parses
	docJSON := `{
		"id": "did:web:example.com",
		"alsoKnownAs": ["at://example.com"],
		"service": [
			{"id": "#didcomm", "type": "DIDCommMessaging", "serviceEndpoint": {"uri": "https://example.com/didcomm"}},
			{"id": "#set", "type": "LinkedDomains", "serviceEndpoint": ["https://a.example.com", "https://b.example.com"]},
			{"id": "#atproto_pds", "type": "AtprotoPersonalDataServer", "serviceEndpoint": "https://pds.example.com"}
		]
	}`

	var doc DIDDocument
	if err := json.Unmarshal([]byte(docJSON), &doc); err != nil {
		t.Fatal(err)
	}
	assert.Equal("did:web:example.com", doc.DID.String())
	assert.Equal([]string{"at://example.com"}, doc.AlsoKnownAs)
	assert.Equal([]DocService{
		{ID: "#atproto_pds", Type: "AtprotoPersonalDataServer", ServiceEndpoint: "https://pds.example.com"},
	}, doc.Service)

	ident := ParseIdentity(&doc)
	assert.Equal("https://pds.example.com", ident.PDSEndpoint())
	assert.Equal(1, len(ident.Services))
}
