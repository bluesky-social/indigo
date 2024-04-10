package search

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeLossyURL(t *testing.T) {
	assert := assert.New(t)

	fixtures := []struct {
		orig  string
		clean string
	}{
		{orig: "", clean: ""},
		{orig: "asdf", clean: "asdf"},
		{orig: "HTTP://bSky.app:80/index.html", clean: "http://bsky.app"},
		{orig: "https://example.com/thing?c=123&utm_campaign=blah&a=first", clean: "https://example.com/thing?a=first&c=123"},
		{orig: "https://example.com/thing?c=123&utm_campaign=blah&a=first", clean: "https://example.com/thing?a=first&c=123"},
		{orig: "http://example.com/foo//bar.html", clean: "http://example.com/foo/bar.html"},
		{orig: "http://example.com/bar.html#section1", clean: "http://example.com/bar.html"},
		{orig: "http://example.com/foo/", clean: "http://example.com/foo"},
		{orig: "http://example.com/", clean: "http://example.com"},
		{orig: "http://example.com/%7Efoo", clean: "http://example.com/~foo"},
		{orig: "http://example.com/foo/./bar/baz/../qux", clean: "http://example.com/foo/bar/qux"},
		{orig: "http://www.example.com/", clean: "http://example.com"},
	}

	for _, fix := range fixtures {
		assert.Equal(fix.clean, NormalizeLossyURL(fix.orig))
	}
}
