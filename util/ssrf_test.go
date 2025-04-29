package util

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPublicOnlyTransport(t *testing.T) {
	t.Skip("skipping local SSRF test")
	assert := assert.New(t)

	c := http.Client{
		Transport: PublicOnlyTransport(),
	}

	{
		_, err := c.Get("http://127.0.0.1:2470/")
		assert.Error(err)
	}

	{
		_, err := c.Get("http://localhost:2470/path")
		assert.Error(err)
	}

	{
		_, err := c.Get("http://bsky.app:8080/path")
		assert.Error(err)
	}
}
