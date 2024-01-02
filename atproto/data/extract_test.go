package data

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtract(t *testing.T) {
	assert := assert.New(t)

	// TODO: should this be an error?
	tp, err := ExtractTypeJSON([]byte(`{
		"type": "com.example.blah",
		"a": 5
	}`))
	assert.NoError(err)
	assert.Equal("", tp)

	tp, err = ExtractTypeJSON([]byte(`{
		"$type": "com.example.blah",
		"a": 5
	}`))
	assert.NoError(err)
	assert.Equal("com.example.blah", tp)

	inFile, err := os.Open("testdata/feedpost_record.cbor")
	if err != nil {
		t.Fail()
	}
	cborBytes, err := io.ReadAll(inFile)
	if err != nil {
		t.Fail()
	}

	tp, err = ExtractTypeCBOR(cborBytes)
	assert.NoError(err)
	assert.Equal("app.bsky.feed.post", tp)
}
