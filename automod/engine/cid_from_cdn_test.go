package engine

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestCidFromCdnUrl(t *testing.T) {
	assert := assert.New(t)

	fixCid := "abcdefghijk"

	fixtures := []struct {
		url string
		cid *string
	}{
		{
			url: "https://cdn.bsky.app/img/avatar/plain/did:plc:abc123/abcdefghijk@jpeg",
			cid: &fixCid,
		},
		{
			url: "https://cdn.bsky.app/img/feed_fullsize/plain/did:plc:abc123/abcdefghijk@jpeg",
			cid: &fixCid,
		},
		{
			url: "https://cdn.bsky.app/img/feed_fullsize",
			cid: nil,
		},
		{
			url: "https://cdn.bsky.app/img/feed_fullsize/plain/did:plc:abc123/abcdefghijk",
			cid: &fixCid,
		},
		{
			url: "https://cdn.asky.app/img/feed_fullsize/plain/did:plc:abc123/abcdefghijk@jpeg",
			cid: nil,
		},
	}

	for _, fix := range fixtures {
		assert.Equal(fix.cid, cidFromCdnUrl(&fix.url))
	}
}
