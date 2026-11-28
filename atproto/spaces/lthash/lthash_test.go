package lthash

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLtHashBasic(t *testing.T) {
	assert := assert.New(t)

	h := New()
	empty := h.Sum256()
	h.Add([]byte("yellow"))
	assert.NotEqual(empty, h.Sum256())
	h.Remove([]byte("yellow"))
	assert.Equal(empty, h.Sum256())

	h.Add([]byte("red"))
	h.Add([]byte("green"))
	h.Add([]byte("blue"))
	o1 := h.Sum256()
	h.Add([]byte("orange"))
	o2 := h.Sum256()
	assert.NotEqual(o1, o2)
	h.Remove([]byte("orange"))
	o3 := h.Sum256()
	assert.Equal(o1, o3)

	// remove an element which has not been added
	h.Remove([]byte("purple"))
	o4 := h.Sum256()
	assert.NotEqual(o1, o4)
	h.Add([]byte("purple"))
	o5 := h.Sum256()
	assert.Equal(o1, o5)

	// adding duplicate element is not a no-op
	h.Add([]byte("red"))
	assert.NotEqual(o3, h.Sum256())
	h.Remove([]byte("red"))
	assert.Equal(o3, h.Sum256())

	b := h.Bytes()
	h2 := Load(b)
	assert.Equal(o1, h2.Sum256())

	h3 := New()
	h3.Add([]byte("green"))
	h3.Add([]byte("red"))
	h3.Add([]byte("blue"))
	assert.Equal(o1, h3.Sum256())

	buf, err := h3.MarshalBinary()
	assert.NoError(err)
	var h4 Hash
	assert.NoError(h4.UnmarshalBinary(buf))
	assert.Equal(o1, h4.Sum256())
	assert.True(h3.Equal(&h4))

	// big element
	big := make([]byte, 30_000)
	for i := range len(big) {
		big[i] = 0x12
	}
	h4.Add(big)
	assert.NotEqual(o1, h4.Sum256())
	assert.False(h.Equal(&h4))
	h4.Remove(big)
	assert.Equal(o1, h4.Sum256())
}

// test against known values from the typescript implementation
func TestLtHashAtproto(t *testing.T) {
	assert := assert.New(t)

	// empty
	h := New()
	assert.Equal("e5a00aa9991ac8a5ee3109844d84a55583bd20572ad3ffcd42792f3c36b183ad", fmt.Sprintf("%x", h.Sum256()))

	// simple elements
	h.Add([]byte("one"))
	h.Add([]byte("two"))
	assert.Equal("ae05cb6d224379d9710c290c8529945c5b0e0fde9ead30b9699057ce701c63e7", fmt.Sprintf("%x", h.Sum256()))

	// TODO: realistic atproto spaces example (collection/rkey/version)
}
