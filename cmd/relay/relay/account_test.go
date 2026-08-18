package relay

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/stretchr/testify/assert"
)

type DIDFixture struct {
	Val  string
	Norm string
}

func TestNormalizeDID(t *testing.T) {
	assert := assert.New(t)

	fixtures := []DIDFixture{
		DIDFixture{Val: "did:web:example.com", Norm: "did:web:example.com"},
		DIDFixture{Val: "did:web:example.com", Norm: "did:web:example.com"},
		DIDFixture{Val: "did:web:EXAMPLE.com", Norm: "did:web:example.com"},
		DIDFixture{Val: "did:plc:ABC123", Norm: "did:plc:abc123"},
		DIDFixture{Val: "did:other:ABC", Norm: "did:other:ABC"},
	}

	for _, f := range fixtures {
		assert.Equal(f.Norm, NormalizeDID(syntax.DID(f.Val)).String())
	}
}
