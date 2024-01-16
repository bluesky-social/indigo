package keyword

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTokenizeText(t *testing.T) {
	assert := assert.New(t)

	fixtures := []struct {
		text string
		out  []string
	}{
		{text: "", out: []string{}},
		{text: "Hello, โลก!", out: []string{"hello", "โลก"}},
		{text: "Gdańsk", out: []string{"gdansk"}},
	}

	for _, fix := range fixtures {
		assert.Equal(fix.out, TokenizeText(fix.text))
	}
}

func TestTokenizeIdentifier(t *testing.T) {
	assert := assert.New(t)

	fixtures := []struct {
		ident string
		out  []string
	}{
		{ident: "", out: []string{}},
		{ident: "the-handle.example.com", out: []string{"the", "handle", "example", "com"}},
		{ident: "@a-b-c", out: []string{}},
	}

	for _, fix := range fixtures {
		assert.Equal(fix.out, TokenizeIdentifier(fix.ident))
	}
}
