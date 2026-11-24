package star

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/hyphacoop/go-dasl/drisl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// write a minimal STAR file, read it back
func TestStarRoundTrip(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	// dummy meta object and data pair
	key := []byte("dummy-test-key")
	val := map[string]any{
		"what": "dummy-test-value",
	}
	metaType := "dummy-meta-type"
	meta := map[string]any{
		"$type":  metaType,
		"number": uint64(123),
		"string": "one",
	}

	// write meta and pair
	buf := bytes.NewBuffer([]byte{})
	writer := NewWriter(buf)
	require.NoError(writer.MarshalMeta(meta))
	require.NoError(writer.MarshalPairCBOR(key, val))
	require.NoError(writer.Close())

	// read back from buffer
	reader := NewReader(buf)
	rtype, err := reader.MetaType()
	require.NoError(err)
	assert.Equal(metaType, rtype)

	var rmeta map[string]any
	require.NoError(reader.UnmarshalMeta(&rmeta))
	assert.Equal(meta, rmeta)

	rkey, rval, err := reader.ReadPair()
	require.NoError(err)
	assert.Equal(key, rkey)
	var rvalMap map[string]any
	require.NoError(drisl.Unmarshal(rval, &rvalMap))
	assert.Equal(val, rvalMap)

	_, _, err = reader.ReadPair()
	assert.Equal(io.EOF, err)
	_, _, err = reader.ReadPair()
	assert.Equal(io.EOF, err)

	return
}

func TestReaderReset(t *testing.T) {
	require := require.New(t)

	f1, err := os.Open("testdata/example.star")
	require.NoError(err)
	defer f1.Close()
	r := NewReader(f1)

	// read some data
	_, _, err = r.ReadPair()
	require.NoError(err)

	// reset mid-read
	f2, err := os.Open("testdata/example.star")
	require.NoError(err)
	defer f2.Close()
	r.Reset(f2)

	// read some data
	_, _, err = r.ReadPair()
	require.NoError(err)
}
