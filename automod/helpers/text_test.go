package helpers

import (
	"testing"

	"github.com/bluesky-social/indigo/automod/keyword"

	"github.com/stretchr/testify/assert"
)

func TestTokenizeText(t *testing.T) {
	assert := assert.New(t)

	fixtures := []struct {
		s   string
		out []string
	}{
		{
			s:   "1 'Two' three!",
			out: []string{"1", "two", "three"},
		},
		{
			s:   "  foo1;bar2,baz3...",
			out: []string{"foo1", "bar2", "baz3"},
		},
		{
			s:   "https://example.com/index.html",
			out: []string{"https", "example", "com", "index", "html"},
		},
	}

	for _, fix := range fixtures {
		assert.Equal(fix.out, keyword.TokenizeText(fix.s))
	}
}

func TestExtractURL(t *testing.T) {
	assert := assert.New(t)

	fixtures := []struct {
		s   string
		out []string
	}{
		{
			s:   "this is a description with example.com mentioned in the middle",
			out: []string{"example.com"},
		},
		{
			s:   "this is another example with https://en.wikipedia.org/index.html: and archive.org, and https://eff.org/... and bsky.app.",
			out: []string{"https://en.wikipedia.org/index.html", "archive.org", "https://eff.org/", "bsky.app"},
		},
	}

	for _, fix := range fixtures {
		assert.Equal(fix.out, ExtractTextURLs(fix.s))
	}
}

func TestHashOfString(t *testing.T) {
	assert := assert.New(t)

	// hashing function should be consistent over time
	assert.Equal("4e6f69c0e3d10992", HashOfString("dummy-value"))
}
