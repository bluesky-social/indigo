package keyword

import (
	"regexp"
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
		{text: " foo1;bar2,baz3...", out: []string{"foo1", "bar2", "baz3"}},
		{text: "foo*bar", out: []string{"foo", "bar"}},
		{text: "foo-bar", out: []string{"foo", "bar"}},
		{text: "foo_bar", out: []string{"foo", "bar"}},
	}

	for _, fix := range fixtures {
		assert.Equal(fix.out, TokenizeText(fix.text))
	}
}

func TestTokenizeTextWithCensorChars(t *testing.T) {
	assert := assert.New(t)

	fixtures := []struct {
		text string
		out  []string
	}{
		{text: "", out: []string{}},
		{text: "Hello, โลก!", out: []string{"hello", "โลก"}},
		{text: "Gdańsk", out: []string{"gdansk"}},
		{text: " foo1;bar2,baz3...", out: []string{"foo1", "bar2", "baz3"}},
		{text: "foo*bar,foo&bar", out: []string{"foo*bar", "foo", "bar"}},
		{text: "foo-bar,foo&bar", out: []string{"foo-bar", "foo", "bar"}},
		{text: "foo_bar,foo&bar", out: []string{"foo_bar", "foo", "bar"}},
		{text: "foo#bar,foo&bar", out: []string{"foo#bar", "foo", "bar"}},
	}

	for _, fix := range fixtures {
		assert.Equal(fix.out, TokenizeTextSkippingCensorChars(fix.text))
	}
}

func TestTokenizeTextWithCustomRegex(t *testing.T) {
	assert := assert.New(t)

	fixtures := []struct {
		text string
		out  []string
	}{
		{text: "", out: []string{}},
		{text: "Hello, โลก!", out: []string{"hello", "โลก"}},
		{text: "Gdańsk", out: []string{"gdansk"}},
		{text: " foo1;bar2,baz3...", out: []string{"foo1", "bar2", "baz3"}},
		{text: "foo*bar", out: []string{"foo", "bar"}},
		{text: "foo&bar,foo*bar", out: []string{"foo&bar", "foo", "bar"}},
	}

	regex := regexp.MustCompile(`[^\pL\pN\s&]`)
	for _, fix := range fixtures {
		assert.Equal(fix.out, TokenizeTextWithRegex(fix.text, regex))
	}
}

func TestTokenizeIdentifier(t *testing.T) {
	assert := assert.New(t)

	fixtures := []struct {
		ident string
		out   []string
	}{
		{ident: "", out: []string{}},
		{ident: "the-handle.example.com", out: []string{"the", "handle", "example", "com"}},
		{ident: "@a-b-c", out: []string{}},
	}

	for _, fix := range fixtures {
		assert.Equal(fix.out, TokenizeIdentifier(fix.ident))
	}
}
