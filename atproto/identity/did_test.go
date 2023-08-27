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
	}
}
