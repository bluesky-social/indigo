package lexicon

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

type QueryFixture struct {
	Name   string `json:"name"`
	Params string `json:"params"`
}

func TestInteropQueryValid(t *testing.T) {
	assert := assert.New(t)

	cat := NewBaseCatalog()
	if err := cat.LoadDirectory("testdata/catalog"); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open("testdata/query-data-valid.json")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	jsonBytes, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}

	var fixtures []QueryFixture
	if err := json.Unmarshal(jsonBytes, &fixtures); err != nil {
		t.Fatal(err)
	}

	for _, fixture := range fixtures {
		fmt.Println(fixture.Name)
		params, err := url.ParseQuery(fixture.Params)
		if err != nil {
			t.Fatal(err)
		}

		assert.NoError(ValidateQuery(&cat, params, "example.lexicon.query", 0))
	}
}

func TestInteropQueryInvalid(t *testing.T) {
	assert := assert.New(t)

	cat := NewBaseCatalog()
	if err := cat.LoadDirectory("testdata/catalog"); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open("testdata/query-data-invalid.json")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	jsonBytes, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}

	var fixtures []QueryFixture
	if err := json.Unmarshal(jsonBytes, &fixtures); err != nil {
		t.Fatal(err)
	}

	for _, fixture := range fixtures {
		fmt.Println(fixture.Name)
		params, err := url.ParseQuery(fixture.Params)
		if err != nil {
			t.Fatal(err)
		}

		err = ValidateQuery(&cat, params, "example.lexicon.query", 0)
		if err == nil {
			fmt.Println("   FAIL")
		}
		assert.Error(err)
	}
}
