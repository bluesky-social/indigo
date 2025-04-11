package relay

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type DIDFixture struct {
	Val  string
	Norm string
}

func TestNormalizeDID(t *testing.T) {
	assert := assert.New(t)

	fixtures := []HostnameFixture{
		DIDFixture{Val: "did:web:example.com", Norm: "did:web:example.com"},
		DIDFixture{Val: "did:web:example.com", Norm: "did:web:example.com"},
		DIDFixture{Val: "did:web:EXAMPLE.com", Norm: "did:web:example.com"},
		DIDFixture{Val: "did:plc:ABC123", Norm: "did:plc:abc123"},
		DIDFixture{Val: "did:other:ABC", Norm: "did:other:ABC"},
	}

	for _, f := range fixtures {
		assert.Equal(f.Norm, NormalizeDID(f.Val))
	}
}
