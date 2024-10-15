package lexicon

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/bluesky-social/indigo/atproto/data"

	"github.com/stretchr/testify/assert"
)

type RecordFixture struct {
	Name      string          `json:"name"`
	RecordKey string          `json:"rkey"`
	Data      json.RawMessage `json:"data"`
}

func TestInteropRecordValid(t *testing.T) {
	assert := assert.New(t)

	cat := NewBaseCatalog()
	if err := cat.LoadDirectory("testdata/catalog"); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open("testdata/record-data-valid.json")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	jsonBytes, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}

	var fixtures []RecordFixture
	if err := json.Unmarshal(jsonBytes, &fixtures); err != nil {
		t.Fatal(err)
	}

	for _, fixture := range fixtures {
		fmt.Println(fixture.Name)
		d, err := data.UnmarshalJSON(fixture.Data)
		if err != nil {
			t.Fatal(err)
		}

		assert.NoError(ValidateRecord(&cat, d, "example.lexicon.record", 0))
	}
}

func TestInteropRecordInvalid(t *testing.T) {
	assert := assert.New(t)

	cat := NewBaseCatalog()
	if err := cat.LoadDirectory("testdata/catalog"); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open("testdata/record-data-invalid.json")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	jsonBytes, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}

	var fixtures []RecordFixture
	if err := json.Unmarshal(jsonBytes, &fixtures); err != nil {
		t.Fatal(err)
	}

	for _, fixture := range fixtures {
		fmt.Println(fixture.Name)
		d, err := data.UnmarshalJSON(fixture.Data)
		if err != nil {
			t.Fatal(err)
		}
		err = ValidateRecord(&cat, d, "example.lexicon.record", 0)
		if err == nil {
			fmt.Println("   FAIL")
		}
		assert.Error(err)
	}
}
