package star

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type starTestCase struct {
	Comment string       `json:"comment"`
	StarHex string       `json:"star_hex"`
	Decoded *decodedStar `json:"decoded,omitempty"`
}

func (tc *starTestCase) StarFile() *bytes.Buffer {
	b, err := hex.DecodeString(tc.StarHex)
	if err != nil {
		panic(err)
	}
	return bytes.NewBuffer(b)
}

type decodedStar struct {
	Meta  map[string]any `json:"meta"`
	Pairs []starPair     `json:"pairs"`
}

type starPair struct {
	KeyHex   string `json:"key_hex"`
	ValueHex string `json:"value_hex"`
}

func (pair *starPair) Key() []byte {
	b, err := hex.DecodeString(pair.KeyHex)
	if err != nil {
		panic(err)
	}
	return b
}

func (pair *starPair) Value() []byte {
	b, err := hex.DecodeString(pair.ValueHex)
	if err != nil {
		panic(err)
	}
	return b
}

func loadStarTestCases(p string) []starTestCase {
	b, err := os.ReadFile(p)
	if err != nil {
		panic(err)
	}
	var fixtures []starTestCase
	if err := json.Unmarshal(b, &fixtures); err != nil {
		panic(err)
	}
	// convert any meta float fields to uint64
	for i := range fixtures {
		if fixtures[i].Decoded == nil || fixtures[i].Decoded.Meta == nil {
			continue
		}
		for k, v := range fixtures[i].Decoded.Meta {
			flt, ok := v.(float64)
			if ok {
				fixtures[i].Decoded.Meta[k] = uint64(flt)
			}
		}
	}
	return fixtures
}

func TestValidInteropVectors(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	fixtures := loadStarTestCases("testdata/valid-star-fixtures.json")

	for _, f := range fixtures {
		reader := NewReader(f.StarFile())

		var meta map[string]any
		err := reader.UnmarshalMeta(&meta)
		require.NoError(err, f.Comment)
		assert.Equal(f.Decoded.Meta, meta, f.Comment)

		for _, pair := range f.Decoded.Pairs {
			key, val, err := reader.ReadPair()
			require.NoError(err, f.Comment)
			assert.Equal(pair.Key(), key, f.Comment)
			assert.Equal(pair.Value(), val, f.Comment)
		}
		_, _, err = reader.ReadPair()
		assert.Equal(io.EOF, err, f.Comment)
	}
}

func TestInvalidInteropVectors(t *testing.T) {
	assert := assert.New(t)

	fixtures := loadStarTestCases("testdata/invalid-star-fixtures.json")

	for _, f := range fixtures {
		reader := NewReader(f.StarFile())
		for reader.Scan() {
			// pass
		}
		assert.Error(reader.Err(), f.Comment)
		assert.False(errors.Is(reader.Err(), io.EOF), f.Comment)
	}
}
