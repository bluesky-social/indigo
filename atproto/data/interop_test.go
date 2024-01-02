package data

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/assert"
)

type DataModelFixture struct {
	JSON       json.RawMessage `json:"json"`
	CBORBase64 string          `json:"cbor_base64"`
	CID        string          `json:"cid"`
}

func TestInteropDataModelFixtures(t *testing.T) {

	f, err := os.Open("testdata/data-model-fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	fixBytes, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}

	var fixtures []DataModelFixture
	if err := json.Unmarshal(fixBytes, &fixtures); err != nil {
		t.Fatal(err)
	}

	for _, row := range fixtures {
		testDataModelFixture(t, row)
	}
}

func testDataModelFixture(t *testing.T, row DataModelFixture) {
	assert := assert.New(t)

	jsonBytes := []byte(row.JSON)
	cborBytes, err := base64.RawStdEncoding.DecodeString(row.CBORBase64)
	if err != nil {
		t.Fatal(err)
	}

	jsonObj, err := UnmarshalJSON(jsonBytes)
	assert.NoError(err)
	cborObj, err := UnmarshalCBOR(cborBytes)
	assert.NoError(err)

	assert.Equal(jsonObj, cborObj)

	cborFromJSON, err := MarshalCBOR(jsonObj)
	assert.NoError(err)
	cborFromCBOR, err := MarshalCBOR(cborObj)
	assert.NoError(err)

	cborObjAgain, err := UnmarshalCBOR(cborFromJSON)
	assert.NoError(err)
	assert.Equal(jsonObj, cborObjAgain)

	assert.Equal(cborBytes, cborFromJSON)
	assert.Equal(cborBytes, cborFromCBOR)

	// 0x71 = dag-cbor, 0x12 = sha2-256, 0 = default length
	cidBuilder := cid.V1Builder{Codec: 0x71, MhType: 0x12, MhLength: 0}
	cidFromJSON, err := cidBuilder.Sum(cborFromJSON)
	assert.NoError(err)
	assert.Equal(row.CID, cidFromJSON.String())
	cidFromCBOR, err := cidBuilder.Sum(cborFromCBOR)
	assert.NoError(err)
	assert.Equal(row.CID, cidFromCBOR.String())

}

type DataModelSimpleFixture struct {
	JSON json.RawMessage `json:"json"`
}

func TestInteropDataModelValid(t *testing.T) {
	assert := assert.New(t)

	f, err := os.Open("testdata/data-model-valid.json")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	fixBytes, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}

	var fixtures []DataModelSimpleFixture
	if err := json.Unmarshal(fixBytes, &fixtures); err != nil {
		t.Fatal(err)
	}

	for _, row := range fixtures {
		_, err := UnmarshalJSON(row.JSON)
		assert.NoError(err)
	}
}

func TestInteropDataModelInvalid(t *testing.T) {
	assert := assert.New(t)

	f, err := os.Open("testdata/data-model-invalid.json")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	fixBytes, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}

	var fixtures []DataModelSimpleFixture
	if err := json.Unmarshal(fixBytes, &fixtures); err != nil {
		t.Fatal(err)
	}

	for _, row := range fixtures {
		_, err := UnmarshalJSON(row.JSON)
		assert.Error(err)
	}
}
