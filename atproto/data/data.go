package data

import (
	"testing"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/assert"
)

type DataModelFixture struct {
	JSON			   json.RawMessage `json:"json"`
	CBORBase64    	   string `json:"cbor_base64"`
	CID 			   string `json:"cid"`
}

func TestDataModelFixtures(t *testing.T) {
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

	assert.Equal(cborBytes, cborFromJSON)
	assert.Equal(cborBytes, cborFromCBOR)

	// XXX: make a helper function
	// 0x71 = dag-cbor, 0x12 = sha2-256, 0 = default length
	cidBuilder := cid.V1Builder{0x71, 0x12, 0}
	cidCBOR, err := cidBuilder.Sum(cborBytes)
	assert.NoError(err)
	assert.Equal(row.CID, cidCBOR.String())

	cidFromJSON, err := cidBuilder.Sum(cborFromJSON)
	assert.NoError(err)
	assert.Equal(row.CID, cidFromJSON.String())

	cidFromCBOR, err := cidBuilder.Sum(cborFromCBOR)
	assert.NoError(err)
	assert.Equal(row.CID, cidFromCBOR.String())
}

func TestInvalidJSON(t *testing.T) {
    assert := assert.New(t)

    paths, err := filepath.Glob("testdata/invalid/*.json")
	if err != nil {
		t.Fatal(err)
	}
    assert.NoError(err)
    _ = paths
}
