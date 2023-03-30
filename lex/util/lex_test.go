package util

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/assert"
	cbg "github.com/whyrusleeping/cbor-gen"
)

type basicSchema struct {
	A string           `json:"a" cborgen:"a"`
	B int64            `json:"b" cborgen:"b"`
	C bool             `json:"c" cborgen:"c"`
	D *string          `json:"d,omitempty" cborgen:"d,omitempty"`
	E *string          `json:"e" cborgen:"e"`
	F []string         `json:"f" cborgen:"f"`
	G basicSchemaInner `json:"g" cborgen:"g"`
}

type basicSchemaInner struct {
	H string   `json:"h" cborgen:"h"`
	I int64    `json:"i" cborgen:"i"`
	J bool     `json:"j" cborgen:"j"`
	K []string `json:"k" cborgen:"k"`
}

type ipldSchema struct {
	A LexLink  `json:"a" cborgen:"a"`
	B LexBytes `json:"b" cborgen:"b"`
}

type ipldNestedSchema struct {
	A ipldNestedSchemaInner `json:"a" cborgen:"a"`
}

type ipldNestedSchemaInner struct {
	B []ipldNestedSchemaInnerInner `json:"b" cborgen:"b"`
}

type ipldNestedSchemaInnerInner struct {
	C *string    `json:"c,omitempty" cborgen:"c,omitempty"`
	D []LexLink  `json:"d" cborgen:"d"`
	E []LexBytes `json:"e" cborgen:"e"`
}

func TestCborGen(t *testing.T) {
	/* XXX:
	if err := cbg.WriteMapEncodersToFile("cbor_gen_test.go", "util", basicSchema{}, basicSchemaInner{}, ipldSchema{}, ipldNestedSchema{}, ipldNestedSchemaInner{}, ipldNestedSchemaInnerInner{}); err != nil {
		t.Fatal(err)
	}
	*/
	if err := cbg.WriteMapEncodersToFile("cbor_gen_test.go", "util", basicSchema{}, basicSchemaInner{}, ipldSchema{}); err != nil {
		t.Fatal(err)
	}
}

func TestInteropBasicSchema(t *testing.T) {
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
	goObj := basicSchema{
		A: "abc",
		B: 123,
		C: true,
		D: nil,
		E: nil,
		F: []string{"abc", "def", "ghi"},
		G: basicSchemaInner{
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
	jsonObj := basicSchema{}
	assert.NoError(json.Unmarshal([]byte(jsonStr), &jsonObj))
	cborObj := basicSchema{}
	assert.NoError(cborObj.UnmarshalCBOR(bytes.NewReader(cborBytes)))

	// compare parsed against known object
	assert.Equal(goObj, jsonObj)
	assert.Equal(goObj, cborObj)

	// reproduce CBOR serialization
	goCborBytes := new(bytes.Buffer)
	assert.NoError(goObj.MarshalCBOR(goCborBytes))
	assert.Equal(cborBytes, goCborBytes.Bytes())
	// 0x71 = dag-cbor, 0x12 = sha2-256, 0 = default length
	cidBuilder := cid.V1Builder{0x71, 0x12, 0}
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

func TestInteropIpldSchema(t *testing.T) {
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
	goObj := ipldSchema{
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
	jsonObj := ipldSchema{}
	assert.NoError(json.Unmarshal([]byte(jsonStr), &jsonObj))
	cborObj := ipldSchema{}
	assert.NoError(cborObj.UnmarshalCBOR(bytes.NewReader(cborBytes)))

	// compare parsed against known object
	assert.Equal(goObj, jsonObj)
	assert.Equal(goObj, cborObj)

	// reproduce CBOR serialization
	goCborBytes := new(bytes.Buffer)
	assert.NoError(goObj.MarshalCBOR(goCborBytes))
	assert.Equal(cborBytes, goCborBytes.Bytes())
	// 0x71 = dag-cbor, 0x12 = sha2-256, 0 = default length
	cidBuilder := cid.V1Builder{0x71, 0x12, 0}
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

func TestInteropIpldNestedSchema(t *testing.T) {
	assert := assert.New(t)

	jsonStr := `{
      "a": {
        "b": [
          {
            "d": [
              {"$link": "bafyreidfayvfuwqa7qlnopdjiqrxzs6blmoeu4rujcjtnci5beludirz2a"},
              {"$link": "bafyreidfayvfuwqa7qlnopdjiqrxzs6blmoeu4rujcjtnci5beludirz2a"}
            ],
            "e": [
              { "$bytes": "nFERjvLLiw9qm45JrqH9QTzyC2Lu1Xb4ne6+sBrCzI0" },
              { "$bytes": "iE+sPoHobU9tSIqGI+309LLCcWQIRmEXwxcoDt19tas" }
            ]
          }
        ]
      }
    }`
	cidOne, err := cid.Decode("bafyreidfayvfuwqa7qlnopdjiqrxzs6blmoeu4rujcjtnci5beludirz2a")
	assert.NoError(err)
	cidTwo, err := cid.Decode("bafyreidfayvfuwqa7qlnopdjiqrxzs6blmoeu4rujcjtnci5beludirz2a")
	assert.NoError(err)
	goObj := ipldNestedSchema{
		A: ipldNestedSchemaInner{
			B: []ipldNestedSchemaInnerInner{
				ipldNestedSchemaInnerInner{
					D: []LexLink{
						LexLink(cidOne),
						LexLink(cidTwo),
					},
					E: []LexBytes{
						LexBytes([]byte{
							156, 81, 17, 142, 242, 203, 139, 15, 106, 155, 142, 73, 174,
							161, 253, 65, 60, 242, 11, 98, 238, 213, 118, 248, 157, 238,
							190, 176, 26, 194, 204, 141,
						}),
						LexBytes([]byte{
							136, 79, 172, 62, 129, 232, 109, 79, 109, 72, 138, 134, 35, 237,
							244, 244, 178, 194, 113, 100, 8, 70, 97, 23, 195, 23, 40, 14,
							221, 125, 181, 171,
						}),
					},
				},
			},
		},
	}
	cborBytes := []byte{161, 97, 97, 161, 97, 98, 129, 162, 97, 100, 130, 216, 42, 88, 37, 0, 1,
		113, 18, 32, 101, 6, 42, 90, 90, 0, 252, 22, 215, 60, 105, 68, 35, 124,
		203, 193, 91, 28, 74, 114, 52, 72, 147, 54, 137, 29, 9, 23, 65, 162, 57,
		208, 216, 42, 88, 37, 0, 1, 113, 18, 32, 101, 6, 42, 90, 90, 0, 252, 22,
		215, 60, 105, 68, 35, 124, 203, 193, 91, 28, 74, 114, 52, 72, 147, 54,
		137, 29, 9, 23, 65, 162, 57, 208, 97, 101, 130, 88, 32, 156, 81, 17, 142,
		242, 203, 139, 15, 106, 155, 142, 73, 174, 161, 253, 65, 60, 242, 11, 98,
		238, 213, 118, 248, 157, 238, 190, 176, 26, 194, 204, 141, 88, 32, 136,
		79, 172, 62, 129, 232, 109, 79, 109, 72, 138, 134, 35, 237, 244, 244, 178,
		194, 113, 100, 8, 70, 97, 23, 195, 23, 40, 14, 221, 125, 181, 171}
	cidStr := "bafyreid3imdulnhgeytpf6uk7zahjvrsqlofkmm5b5ub2maw4kqus6jp4i"

	// easier commenting out of code during development
	_ = assert
	_ = jsonStr
	_ = goObj
	_ = cidStr
	_ = bytes.NewReader(cborBytes)

	// basic parsing
	jsonObj := ipldNestedSchema{}
	assert.NoError(json.Unmarshal([]byte(jsonStr), &jsonObj))
	//cborObj := ipldNestedSchema{}
	//assert.NoError(cborObj.UnmarshalCBOR(bytes.NewReader(cborBytes)))

	// compare parsed against known object
	assert.Equal(goObj, jsonObj)
	//assert.Equal(goObj, cborObj)

	// reproduce CBOR serialization
	/* XXX
	goCborBytes := new(bytes.Buffer)
	assert.NoError(goObj.MarshalCBOR(goCborBytes))
	assert.Equal(cborBytes, goCborBytes.Bytes())
	// 0x71 = dag-cbor, 0x12 = sha2-256, 0 = default length
	cidBuilder := cid.V1Builder{ 0x71, 0x12, 0 }
	goCborCid, err := cidBuilder.Sum(goCborBytes.Bytes())
	assert.NoError(err)
	assert.Equal(cidStr, goCborCid.String())
	*/

	// reproduce JSON serialization
	var jsonAll interface{}
	assert.NoError(json.Unmarshal([]byte(jsonStr), &jsonAll))
	goJsonBytes, err := json.Marshal(goObj)
	assert.NoError(err)
	var goJsonAll interface{}
	assert.NoError(json.Unmarshal(goJsonBytes, &goJsonAll))
	assert.Equal(jsonAll, goJsonAll)
}
