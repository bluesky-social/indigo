package identity

import (
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests parsing and normalizing handles from DID documents
func TestHandleExtraction(t *testing.T) {
	assert := assert.New(t)
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

	{
		ident := ParseIdentity(&doc)
		hdl, err := ident.DeclaredHandle()
		assert.NoError(err)
		assert.Equal("atproto.com", hdl.String())
	}

	{
		doc.AlsoKnownAs = []string{
			"at://BLAH.com",
			"at://other.org",
		}
		ident := ParseIdentity(&doc)
		hdl, err := ident.DeclaredHandle()
		assert.NoError(err)
		assert.Equal("blah.com", hdl.String())
	}

	{
		doc.AlsoKnownAs = []string{
			"https://http.example.com",
			"at://under_example_com",
			"at://correct.EXAMPLE.com",
			"at://other.example.com",
		}
		ident := ParseIdentity(&doc)
		hdl, err := ident.DeclaredHandle()
		assert.NoError(err)
		assert.Equal("correct.example.com", hdl.String())
	}

	{
		doc.AlsoKnownAs = []string{
			"https://http.example.com",
		}
		ident := ParseIdentity(&doc)
		_, err := ident.DeclaredHandle()
		assert.Error(err)
		assert.Equal("handle.invalid", ident.Handle.String())
	}
}
