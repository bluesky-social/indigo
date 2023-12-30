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
	String  string           `json:"string" cborgen:"string"`
	Unicode string           `json:"unicode" cborgen:"unicode"`
	Integer int64            `json:"integer" cborgen:"integer"`
	Bool    bool             `json:"bool" cborgen:"bool"`
	Null    *string          `json:"null" cborgen:"null"`
	Absent  *string          `json:"absent,omitempty" cborgen:"absent,omitempty"`
	Array   []string         `json:"array" cborgen:"array"`
	Object  basicSchemaInner `json:"object" cborgen:"object"`
}

type basicSchemaInner struct {
	String string   `json:"string" cborgen:"string"`
	Number int64    `json:"number" cborgen:"number"`
	Bool   bool     `json:"bool" cborgen:"bool"`
	Arr    []string `json:"arr" cborgen:"arr"`
}

type ipldSchema struct {
	A LexLink  `json:"a" cborgen:"a"`
	B LexBytes `json:"b" cborgen:"b"`
	C LexBlob  `json:"c" cborgen:"c"`
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
	if err := cbg.WriteMapEncodersToFile("cbor_gen_test.go", "util", basicSchema{}, basicSchemaInner{}, ipldSchema{}, basicOldSchema{}, basicOldSchemaInner{}, ipldOldSchema{}); err != nil {
		t.Fatal(err)
	}
}

func TestInteropBasicSchema(t *testing.T) {
	assert := assert.New(t)
	jsonStr := `{
      "string": "abc",
      "unicode": "a~öñ©⽘☎𓋓😀👨‍👩‍👧‍👧",
      "integer": 123,
      "bool": true,
      "null": null,
      "array": ["abc", "def", "ghi"],
      "object": {
        "string": "abc",
        "number": 123,
        "bool": true,
        "arr": ["abc", "def", "ghi"]
      }
    }`
	goObj := basicSchema{
		String:  "abc",
		Unicode: "a~öñ©⽘☎𓋓😀👨‍👩‍👧‍👧",
		Integer: 123,
		Bool:    true,
		Null:    nil,
		Absent:  nil,
		Array:   []string{"abc", "def", "ghi"},
		Object: basicSchemaInner{
			String: "abc",
			Number: 123,
			Bool:   true,
			Arr:    []string{"abc", "def", "ghi"},
		},
	}
	cborBytes := []byte{
		167, 100, 98, 111, 111, 108, 245, 100, 110, 117, 108, 108, 246, 101, 97,
		114, 114, 97, 121, 131, 99, 97, 98, 99, 99, 100, 101, 102, 99, 103, 104,
		105, 102, 111, 98, 106, 101, 99, 116, 164, 99, 97, 114, 114, 131, 99, 97,
		98, 99, 99, 100, 101, 102, 99, 103, 104, 105, 100, 98, 111, 111, 108, 245,
		102, 110, 117, 109, 98, 101, 114, 24, 123, 102, 115, 116, 114, 105, 110,
		103, 99, 97, 98, 99, 102, 115, 116, 114, 105, 110, 103, 99, 97, 98, 99,
		103, 105, 110, 116, 101, 103, 101, 114, 24, 123, 103, 117, 110, 105, 99,
		111, 100, 101, 120, 47, 97, 126, 195, 182, 195, 177, 194, 169, 226, 189,
		152, 226, 152, 142, 240, 147, 139, 147, 240, 159, 152, 128, 240, 159, 145,
		168, 226, 128, 141, 240, 159, 145, 169, 226, 128, 141, 240, 159, 145, 167,
		226, 128, 141, 240, 159, 145, 167,
	}
	cidStr := "bafyreiclp443lavogvhj3d2ob2cxbfuscni2k5jk7bebjzg7khl3esabwq"

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

func TestInteropIpldSchema(t *testing.T) {
	assert := assert.New(t)

	jsonStr := `{
      "a": {
        "$link": "bafyreidfayvfuwqa7qlnopdjiqrxzs6blmoeu4rujcjtnci5beludirz2a"
      },
      "b": {
        "$bytes": "nFERjvLLiw9qm45JrqH9QTzyC2Lu1Xb4ne6+sBrCzI0"
      },
      "c": {
        "$type": "blob",
        "ref": {
        	"$link": "bafkreiccldh766hwcnuxnf2wh6jgzepf2nlu2lvcllt63eww5p6chi4ity"
        },
        "mimeType": "image/jpeg",
        "size": 10000
      }
    }`
	cidOne, err := cid.Decode("bafyreidfayvfuwqa7qlnopdjiqrxzs6blmoeu4rujcjtnci5beludirz2a")
	assert.NoError(err)
	cidTwo, err := cid.Decode("bafkreiccldh766hwcnuxnf2wh6jgzepf2nlu2lvcllt63eww5p6chi4ity")
	assert.NoError(err)
	goObj := ipldSchema{
		A: LexLink(cidOne),
		B: LexBytes([]byte{
			156, 81, 17, 142, 242, 203, 139, 15, 106, 155, 142, 73, 174, 161, 253,
			65, 60, 242, 11, 98, 238, 213, 118, 248, 157, 238, 190, 176, 26, 194,
			204, 141,
		}),
		C: LexBlob{
			Ref:      LexLink(cidTwo),
			MimeType: "image/jpeg",
			Size:     10000,
		},
	}
	cborBytes := []byte{163, 97, 97, 216, 42, 88, 37, 0, 1, 113, 18, 32, 101, 6, 42, 90, 90, 0,
		252, 22, 215, 60, 105, 68, 35, 124, 203, 193, 91, 28, 74, 114, 52, 72,
		147, 54, 137, 29, 9, 23, 65, 162, 57, 208, 97, 98, 88, 32, 156, 81, 17,
		142, 242, 203, 139, 15, 106, 155, 142, 73, 174, 161, 253, 65, 60, 242, 11,
		98, 238, 213, 118, 248, 157, 238, 190, 176, 26, 194, 204, 141, 97, 99,
		164, 99, 114, 101, 102, 216, 42, 88, 37, 0, 1, 85, 18, 32, 66, 88, 207,
		255, 120, 246, 19, 105, 118, 151, 86, 63, 146, 108, 145, 229, 211, 87, 77,
		46, 162, 90, 231, 237, 146, 214, 235, 252, 35, 163, 136, 158, 100, 115,
		105, 122, 101, 25, 39, 16, 101, 36, 116, 121, 112, 101, 100, 98, 108, 111,
		98, 104, 109, 105, 109, 101, 84, 121, 112, 101, 106, 105, 109, 97, 103,
		101, 47, 106, 112, 101, 103}
	cidStr := "bafyreihldkhcwijkde7gx4rpkkuw7pl6lbyu5gieunyc7ihactn5bkd2nm"

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

	// compare parsed against known object
	assert.Equal(goObj, jsonObj)

	// reproduce JSON serialization
	var jsonAll interface{}
	assert.NoError(json.Unmarshal([]byte(jsonStr), &jsonAll))
	goJsonBytes, err := json.Marshal(goObj)
	assert.NoError(err)
	var goJsonAll interface{}
	assert.NoError(json.Unmarshal(goJsonBytes, &goJsonAll))
	assert.Equal(jsonAll, goJsonAll)

	// TODO: CBOR codegen and validation with array-of-byte-arrays
	/*
		cborObj := ipldNestedSchema{}
		assert.NoError(cborObj.UnmarshalCBOR(bytes.NewReader(cborBytes)))
		assert.Equal(goObj, cborObj)
		goCborBytes := new(bytes.Buffer)
		assert.NoError(goObj.MarshalCBOR(goCborBytes))
		assert.Equal(cborBytes, goCborBytes.Bytes())
		// 0x71 = dag-cbor, 0x12 = sha2-256, 0 = default length
		cidBuilder := cid.V1Builder{Codec: 0x71, MhType: 0x12, MhLength: 0}
		goCborCid, err := cidBuilder.Sum(goCborBytes.Bytes())
		assert.NoError(err)
		assert.Equal(cidStr, goCborCid.String())
	*/
}
