package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWebsocketUrlForHost(t *testing.T) {
	assert := assert.New(t)

	testCases := []struct {
		host     string
		expected string
	}{
		{"localhost", "ws://localhost"},
		{"127.0.0.1", "ws://127.0.0.1"},
		{"[::1]", "ws://[::1]"},
		{"wss://127.0.0.1:443", "wss://127.0.0.1:443"},
		{"example.com", "wss://example.com"},
		{"ws://example.com", "ws://example.com"},
		{"wss://example.com", "wss://example.com"},
		{"http://example.com", "ws://example.com"},
		{"https://example.com", "wss://example.com"},
		{"http://example.com:123", "ws://example.com:123"},
		{"ftp://example.com", "ftp://example.com"},
		{"", ""},
		{"a", "wss://a"},
	}

	for _, c := range testCases {
		assert.Equal(c.expected, WebsocketUrlForHost(c.host))
	}
}
