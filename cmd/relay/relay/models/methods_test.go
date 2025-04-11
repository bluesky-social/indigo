package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHostURLs(t *testing.T) {
	assert := assert.New(t)

	h := Host{
		Hostname: "pds.example.com",
		NoSSL:    false,
	}

	assert.Equal("https://pds.example.com", h.BaseURL())
	assert.Equal("wss://pds.example.com/xrpc/com.atproto.sync.subscribeRepos", h.SubscribeReposURL())

	h.NoSSL = true
	assert.Equal("http://pds.example.com", h.BaseURL())
	assert.Equal("ws://pds.example.com/xrpc/com.atproto.sync.subscribeRepos", h.SubscribeReposURL())

	lh := Host{
		Hostname: "localhost:4321",
		NoSSL:    true,
	}

	assert.Equal("http://localhost:4321", lh.BaseURL())
	assert.Equal("ws://localhost:4321/xrpc/com.atproto.sync.subscribeRepos", lh.SubscribeReposURL())
}
