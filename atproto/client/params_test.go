package client

import (
	"net/url"
	"testing"

	"github.com/gander-social/gander-indigo-sovereign/atproto/syntax"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseParams(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	{
		input := map[string]any{
			"int":       int(-1),
			"uint32":    uint32(32),
			"str":       "hello",
			"bool":      true,
			"did":       syntax.DID("did:web:example.com"),
			"multiBool": []bool{true, false},
			"multiDID":  []syntax.DID{syntax.DID("did:web:example.com"), syntax.DID("did:web:other.com")},
		}
		expect := url.Values(map[string][]string{
			"int":       []string{"-1"},
			"uint32":    []string{"32"},
			"str":       []string{"hello"},
			"bool":      []string{"true"},
			"did":       []string{"did:web:example.com"},
			"multiBool": []string{"true", "false"},
			"multiDID":  []string{"did:web:example.com", "did:web:other.com"},
		})
		output, err := ParseParams(input)
		require.NoError(err)
		assert.Equal(expect, output)
	}

	{
		// unsupported type
		input := map[string]any{
			"map": map[string]int{"a": 123},
		}
		_, err := ParseParams(input)
		assert.Error(err)
	}

}
