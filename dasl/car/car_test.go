package car

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/hyphacoop/go-dasl/drisl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// write a minimal CAR file, read it back
func TestCarRoundTrip(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	// dummy meta object and data pair
	val := map[string]any{
		"what":   "dummy-test-value",
		"number": uint64(123),
		"string": "one",
	}

	// write meta and pair
	buf := bytes.NewBuffer([]byte{})
	writer := NewWriter(buf)
	require.NoError(writer.MarshalRootCBOR(val))
	require.NoError(writer.Flush())

	// read back from buffer
	reader := NewReader(buf)
	_, err := reader.Header()
	require.NoError(err)

	_, rval, err := reader.ReadBlock()
	require.NoError(err)
	var rvalMap map[string]any
	require.NoError(drisl.Unmarshal(rval, &rvalMap))
	assert.Equal(val, rvalMap)

	_, _, err = reader.ReadBlock()
	assert.Equal(io.EOF, err)
	_, _, err = reader.ReadBlock()
	assert.Equal(io.EOF, err)

	return
}

func TestReaderReset(t *testing.T) {
	require := require.New(t)

	f1, err := os.Open("testdata/divy.repo")
	require.NoError(err)
	defer f1.Close()
	r := NewReader(f1)

	// read some data
	_, _, err = r.ReadBlock()
	require.NoError(err)

	// reset mid-read
	f2, err := os.Open("testdata/divy.repo")
	require.NoError(err)
	defer f2.Close()
	r.Reset(f2)

	// read some data
	_, _, err = r.ReadBlock()
	require.NoError(err)
}
