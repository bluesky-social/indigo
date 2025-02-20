package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseServiceURL(t *testing.T) {
	assert := assert.New(t)

	testVec := [][]string{
		{"", ""},
		{"atproto.com", "atproto.com"},
		{"https://bsky.app/profile/atproto.com", "atproto.com"},
		{"https://bsky.app/profile/did:plc:ewvi7nxzyoun6zhxrhs64oiz", "did:plc:ewvi7nxzyoun6zhxrhs64oiz"},
		{"https://bsky.app/profile/atproto.com/post/3lffzv6f4o22r", "at://atproto.com/app.bsky.feed.post/3lffzv6f4o22r"},
	}

	for _, pair := range testVec {
		assert.Equal(pair[1], ParseServiceURL(pair[0]))
	}
}
