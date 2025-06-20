package client

import (
	"testing"

	"github.com/gander-social/gander-indigo-sovereign/atproto/syntax"

	"github.com/stretchr/testify/assert"
)

func TestEncodeLabelerHeader(t *testing.T) {
	assert := assert.New(t)

	labelerA := syntax.DID("did:web:aaa.example.com")
	labelerB := syntax.DID("did:web:bbb.example.com")

	assert.Equal("", encodeLabelerHeader(nil, nil))
	assert.Equal("did:web:aaa.example.com,did:web:bbb.example.com", encodeLabelerHeader(nil, []syntax.DID{labelerA, labelerB}))
	assert.Equal("did:web:aaa.example.com;redact,did:web:bbb.example.com", encodeLabelerHeader([]syntax.DID{labelerA}, []syntax.DID{labelerB}))
	assert.Equal("did:web:aaa.example.com;redact", encodeLabelerHeader([]syntax.DID{labelerA}, nil))
}
