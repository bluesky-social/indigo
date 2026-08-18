package syntax

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRepoPath(t *testing.T) {
	assert := assert.New(t)

	testValid := [][]string{
		{"app.bsky.feed.post/asdf", "app.bsky.feed.post", "asdf"},
	}

	testErr := []string{
		"",
		"/",
		"/app.bsky.feed.post/asdf",
		"/asdf",
		"./app.bsky.feed.post",
		"blob/asdf",
		"app.bsky.feed.post/",
		"app.bsky.feed.post/.",
		"app.bsky.feed.post/!",
	}

	for _, parts := range testValid {
		nsid, rkey, err := ParseRepoPath(parts[0])
		assert.NoError(err)
		assert.Equal(parts[1], nsid.String())
		assert.Equal(parts[2], rkey.String())
	}

	for _, raw := range testErr {
		nsid, rkey, err := ParseRepoPath(raw)
		assert.Error(err)
		assert.Equal("", nsid.String())
		assert.Equal("", rkey.String())
	}
}
