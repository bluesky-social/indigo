// This file has "old" versions of the interop tests. Might as well keep these
// around!

package util

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/assert"
)

type basicOldSchema struct {
	A string              `json:"a" cborgen:"a"`
	B int64               `json:"b" cborgen:"b"`
	C bool                `json:"c" cborgen:"c"`
	D *string             `json:"d,omitempty" cborgen:"d,omitempty"`
	E *string             `json:"e" cborgen:"e"`
	F []string            `json:"f" cborgen:"f"`
	G basicOldSchemaInner `json:"g" cborgen:"g"`
}

type basicOldSchemaInner struct {
	H string   `json:"h" cborgen:"h"`
	I int64    `json:"i" cborgen:"i"`
	J bool     `json:"j" cborgen:"j"`
	K []string `json:"k" cborgen:"k"`
}

type ipldOldSchema struct {
	A LexLink  `json:"a" cborgen:"a"`
	B LexBytes `json:"b" cborgen:"b"`
}

func TestInteropBasicOldSchema(t *testing.T) {
	assert := assert.New(t)
	jsonStr := `{
      "a": "abc",
      "b": 123,
      "c": true,
      "e": null,
      "f": ["abc", "def", "ghi"],
      "g": {
        "h": "abc",
        "i": 123,
        "j": true,
        "k": ["abc", "def", "ghi"]
      }
    }`
	goObj := basicOldSchema{
		A: "abc",
		B: 123,
		C: true,
		D: nil,
		E: nil,
		F: []string{"abc", "def", "ghi"},
		G: basicOldSchemaInner{
			H: "abc",
			I: 123,
			J: true,
			K: []string{"abc", "def", "ghi"},
		},
	}
	cborBytes := []byte{166, 97, 97, 99, 97, 98, 99, 97, 98, 24, 123, 97, 99, 245, 97, 101, 246,
		97, 102, 131, 99, 97, 98, 99, 99, 100, 101, 102, 99, 103, 104, 105, 97,
		103, 164, 97, 104, 99, 97, 98, 99, 97, 105, 24, 123, 97, 106, 245, 97,
		107, 131, 99, 97, 98, 99, 99, 100, 101, 102, 99, 103, 104, 105}
	cidStr := "bafyreiaioukcatdbdltzqznmyqmwgpgmoex62tkwqtdmganxgv3v5bn2o4"

	// easier commenting out of code during development
	_ = assert
	_ = jsonStr
	_ = goObj
	_ = cidStr
	_ = bytes.NewReader(cborBytes)

	// basic parsing
	jsonObj := basicOldSchema{}
	assert.NoError(json.Unmarshal([]byte(jsonStr), &jsonObj))
	cborObj := basicOldSchema{}
	assert.NoError(cborObj.UnmarshalCBOR(bytes.NewReader(cborBytes)))

	// compare parsed against known object
	assert.Equal(goObj, jsonObj)
	assert.Equal(goObj, cborObj)

	// reproduce CBOR serialization
	goCborBytes := new(bytes.Buffer)
	assert.NoError(goObj.MarshalCBOR(goCborBytes))
	assert.Equal(cborBytes, goCborBytes.Bytes())
	// 0x71 = dag-cbor, 0x12 = sha2-256, 0 = default length
	cidBuilder := cid.V1Builder{Codec: 0x71, MhType: 0x12, MhLength: 0}
	goCborCid, err := cidBuilder.Sum(goCborBytes.Bytes())
	assert.NoError(err)
	assert.Equal(cidStr, goCborCid.String())

	// reproduce JSON serialization
	var jsonAll interface{}
	assert.NoError(json.Unmarshal([]byte(jsonStr), &jsonAll))
	goJsonBytes, err := json.Marshal(goObj)
	assert.NoError(err)
	var goJsonAll interface{}
	assert.NoError(json.Unmarshal(goJsonBytes, &goJsonAll))
	assert.Equal(jsonAll, goJsonAll)
}

func TestInteropIpldOldSchema(t *testing.T) {
	assert := assert.New(t)

	jsonStr := `{
      "a": {
        "$link": "bafyreidfayvfuwqa7qlnopdjiqrxzs6blmoeu4rujcjtnci5beludirz2a"
      },
      "b": {
        "$bytes": "nFERjvLLiw9qm45JrqH9QTzyC2Lu1Xb4ne6+sBrCzI0"
      }
    }`
	cidOne, err := cid.Decode("bafyreidfayvfuwqa7qlnopdjiqrxzs6blmoeu4rujcjtnci5beludirz2a")
	assert.NoError(err)
	goObj := ipldOldSchema{
		A: LexLink(cidOne),
		B: LexBytes([]byte{
			156, 81, 17, 142, 242, 203, 139, 15, 106, 155, 142, 73, 174, 161, 253,
			65, 60, 242, 11, 98, 238, 213, 118, 248, 157, 238, 190, 176, 26, 194,
			204, 141,
		}),
	}
	cborBytes := []byte{162, 97, 97, 216, 42, 88, 37, 0, 1, 113, 18, 32, 101, 6, 42, 90, 90, 0,
		252, 22, 215, 60, 105, 68, 35, 124, 203, 193, 91, 28, 74, 114, 52, 72,
		147, 54, 137, 29, 9, 23, 65, 162, 57, 208, 97, 98, 88, 32, 156, 81, 17,
		142, 242, 203, 139, 15, 106, 155, 142, 73, 174, 161, 253, 65, 60, 242, 11,
		98, 238, 213, 118, 248, 157, 238, 190, 176, 26, 194, 204, 141}
	cidStr := "bafyreif37nlcsyb4ckm2i7y3w2ctnebzkib7ekzmv3tvl3mo5og2gxot5y"

	// easier commenting out of code during development
	_ = assert
	_ = jsonStr
	_ = goObj
	_ = cidStr
	_ = bytes.NewReader(cborBytes)

	// basic parsing
	jsonObj := ipldOldSchema{}
	assert.NoError(json.Unmarshal([]byte(jsonStr), &jsonObj))
	cborObj := ipldOldSchema{}
	assert.NoError(cborObj.UnmarshalCBOR(bytes.NewReader(cborBytes)))

	// compare parsed against known object
	assert.Equal(goObj, jsonObj)
	assert.Equal(goObj, cborObj)

	// reproduce CBOR serialization
	goCborBytes := new(bytes.Buffer)
	assert.NoError(goObj.MarshalCBOR(goCborBytes))
	assert.Equal(cborBytes, goCborBytes.Bytes())
	// 0x71 = dag-cbor, 0x12 = sha2-256, 0 = default length
	cidBuilder := cid.V1Builder{Codec: 0x71, MhType: 0x12, MhLength: 0}
	goCborCid, err := cidBuilder.Sum(goCborBytes.Bytes())
	assert.NoError(err)
	assert.Equal(cidStr, goCborCid.String())

	// reproduce JSON serialization
	var jsonAll interface{}
	assert.NoError(json.Unmarshal([]byte(jsonStr), &jsonAll))
	goJsonBytes, err := json.Marshal(goObj)
	assert.NoError(err)
	var goJsonAll interface{}
	assert.NoError(json.Unmarshal(goJsonBytes, &goJsonAll))
	assert.Equal(jsonAll, goJsonAll)
}
