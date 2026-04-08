package util

import (
	"encoding/json"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/assert"
)

type blobSchema struct {
	A string  `json:"a" cborgen:"a"`
	B LexBlob `json:"b" cborgen:"b"`
}

func TestBlobParse(t *testing.T) {
	assert := assert.New(t)
	jsonStr := `{
		"a": "abc",
		"b": {
			"$type": "blob",
			"ref": {
				"$link": "bafyreidfayvfuwqa7qlnopdjiqrxzs6blmoeu4rujcjtnci5beludirz2a"
		},
		"mimeType": "image/png",
		"size": 12345
		}
	}`
	cidOne, err := cid.Decode("bafyreidfayvfuwqa7qlnopdjiqrxzs6blmoeu4rujcjtnci5beludirz2a")
	goObj := blobSchema{
		A: "abc",
		B: LexBlob{
			Ref:      LexLink(cidOne),
			MimeType: "image/png",
			Size:     12345,
		},
	}
	jsonStrLegacy := `{
		"a": "abc",
		"b": {
			"cid": "bafyreidfayvfuwqa7qlnopdjiqrxzs6blmoeu4rujcjtnci5beludirz2a",
			"mimeType": "image/png"
		}
    }`
	goObjLegacy := blobSchema{
		A: "abc",
		B: LexBlob{
			Ref:      LexLink(cidOne),
			MimeType: "image/png",
			Size:     -1,
		},
	}

	// basic parsing
	jsonObj := blobSchema{}
	assert.NoError(json.Unmarshal([]byte(jsonStr), &jsonObj))
	jsonObjLegacy := blobSchema{}
	assert.NoError(json.Unmarshal([]byte(jsonStrLegacy), &jsonObjLegacy))

	// compare parsed against known object
	assert.Equal(goObj, jsonObj)
	assert.Equal(goObjLegacy, jsonObjLegacy)

	// reproduce JSON serialization
	var jsonAll any
	assert.NoError(json.Unmarshal([]byte(jsonStr), &jsonAll))
	goJsonBytes, err := json.Marshal(goObj)
	assert.NoError(err)
	var goJsonAll any
	assert.NoError(json.Unmarshal(goJsonBytes, &goJsonAll))
	assert.Equal(jsonAll, goJsonAll)

	var jsonAllLegacy any
	assert.NoError(json.Unmarshal([]byte(jsonStrLegacy), &jsonAllLegacy))
	goJsonBytesLegacy, err := json.Marshal(goObjLegacy)
	assert.NoError(err)
	var goJsonAllLegacy any
	assert.NoError(json.Unmarshal(goJsonBytesLegacy, &goJsonAllLegacy))
	assert.Equal(jsonAllLegacy, goJsonAllLegacy)
}
