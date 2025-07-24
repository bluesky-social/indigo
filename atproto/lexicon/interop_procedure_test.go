package lexicon

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"testing"

	"github.com/bluesky-social/indigo/atproto/data"
	"github.com/stretchr/testify/assert"
)

type ProcedureFixture struct {
	Name   string          `json:"name"`
	Params string          `json:"params"`
	Body   json.RawMessage `json:"body"`
}

func TestInteropProcedureValid(t *testing.T) {
	assert := assert.New(t)

	cat := NewBaseCatalog()
	if err := cat.LoadDirectory("testdata/catalog"); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open("testdata/procedure-data-valid.json")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	jsonBytes, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}

	var fixtures []ProcedureFixture
	if err := json.Unmarshal(jsonBytes, &fixtures); err != nil {
		t.Fatal(err)
	}

	for _, fixture := range fixtures {
		fmt.Println(fixture.Name)
		params, err := url.ParseQuery(fixture.Params)
		if err != nil {
			t.Fatal(err)
		}
		body, err := data.UnmarshalJSON(fixture.Body)
		if err != nil {
			t.Fatal(err)
		}

		assert.NoError(ValidateProcedure(&cat, body, params, "example.lexicon.procedure", 0))
	}
}

func TestInteropProcedureInvalid(t *testing.T) {
	assert := assert.New(t)

	cat := NewBaseCatalog()
	if err := cat.LoadDirectory("testdata/catalog"); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open("testdata/procedure-data-invalid.json")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	jsonBytes, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}

	var fixtures []ProcedureFixture
	if err := json.Unmarshal(jsonBytes, &fixtures); err != nil {
		t.Fatal(err)
	}

	for _, fixture := range fixtures {
		fmt.Println(fixture.Name)
		params, err := url.ParseQuery(fixture.Params)
		if err != nil {
			t.Fatal(err)
		}
		body, err := data.UnmarshalJSON(fixture.Body)
		if err != nil {
			t.Fatal(err)
		}

		err = ValidateProcedure(&cat, body, params, "example.lexicon.procedure", 0)
		if err == nil {
			fmt.Println("   FAIL")
		}
		assert.Error(err)
	}
}
