package lthash

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	// adding duplicate element is not a no-op
	h.Add([]byte("red"))
	assert.NotEqual(o3, h.Sum256())
	h.Remove([]byte("red"))
	assert.Equal(o3, h.Sum256())

	var buf [2048]byte
	h.Export2(buf[:])

	buf2 := h.Export()
	require.Equal(t, buf, buf2)

	h2 := Load(buf)
	assert.Equal(o1, h2.Sum256())

	h3 := New()
	h3.Add([]byte("green"))
	h3.Add([]byte("red"))
	h3.Add([]byte("blue"))
	assert.Equal(o1, h3.Sum256())
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
}

func TestLtHashReset(t *testing.T) {
	assert := assert.New(t)

	var h Hash
	h.Reset()
	h.Add([]byte("one"))
	h.Add([]byte("two"))
	assert.Equal("ae05cb6d224379d9710c290c8529945c5b0e0fde9ead30b9699057ce701c63e7", fmt.Sprintf("%x", h.Sum256()))

	h.Reset()
	assert.Equal([2048]byte{}, h.Export())
	assert.Equal("e5a00aa9991ac8a5ee3109844d84a55583bd20572ad3ffcd42792f3c36b183ad", fmt.Sprintf("%x", h.Sum256()))

	h.Add([]byte("two"))
	h.Add([]byte("one"))
	assert.Equal("ae05cb6d224379d9710c290c8529945c5b0e0fde9ead30b9699057ce701c63e7", fmt.Sprintf("%x", h.Sum256()))
}
