package lexicon

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atdata"

	"github.com/stretchr/testify/assert"
)

type QueryParamsFixture struct {
	Name string          `json:"name"`
	Data json.RawMessage `json:"data"`
}

type ProcedureInputFixture struct {
	Name string          `json:"name"`
	Ref  string          `json:"ref"`
	Data json.RawMessage `json:"data"`
}

func TestValidateQueryParams(t *testing.T) {
	assert := assert.New(t)

	cat := NewBaseCatalog()
	if err := cat.LoadDirectory("testdata/catalog"); err != nil {
		t.Fatal(err)
	}

	// Test basic successful validation with required field
	assert.NoError(ValidateQueryParams(&cat, map[string]any{
		"stringField": "hello",
	}, "example.lexicon.query", 0))

	// Test with all fields populated
	assert.NoError(ValidateQueryParams(&cat, map[string]any{
		"boolean":     true,
		"integer":     int64(42),
		"stringField": "hello",
		"handle":      "user.bsky.social",
		"array":       []any{int64(1), int64(2), int64(3)},
	}, "example.lexicon.query", 0))

	// Test with minimal query (no parameters defined)
	assert.NoError(ValidateQueryParams(&cat, nil, "example.lexicon.minimal.query", 0))
	assert.NoError(ValidateQueryParams(&cat, map[string]any{}, "example.lexicon.minimal.query", 0))
}

func TestValidateQueryParamsErrors(t *testing.T) {
	assert := assert.New(t)

	cat := NewBaseCatalog()
	if err := cat.LoadDirectory("testdata/catalog"); err != nil {
		t.Fatal(err)
	}

	// Missing required field
	err := ValidateQueryParams(&cat, map[string]any{
		"boolean": true,
	}, "example.lexicon.query", 0)
	assert.Error(err)
	assert.Contains(err.Error(), "required parameter missing: stringField")

	// Wrong schema type (record instead of query)
	err = ValidateQueryParams(&cat, map[string]any{}, "example.lexicon.record", 0)
	assert.Error(err)
	assert.Contains(err.Error(), "not of query type")

	// Wrong type for boolean
	err = ValidateQueryParams(&cat, map[string]any{
		"stringField": "test",
		"boolean":     "true",
	}, "example.lexicon.query", 0)
	assert.Error(err)
	assert.Contains(err.Error(), "parameter boolean")

	// Wrong type for integer
	err = ValidateQueryParams(&cat, map[string]any{
		"stringField": "test",
		"integer":     "42",
	}, "example.lexicon.query", 0)
	assert.Error(err)
	assert.Contains(err.Error(), "parameter integer")

	// Minimal query with unexpected data
	err = ValidateQueryParams(&cat, map[string]any{
		"unexpected": "data",
	}, "example.lexicon.minimal.query", 0)
	assert.Error(err)
	assert.Contains(err.Error(), "no parameters defined")
}

func TestValidateProcedureParams(t *testing.T) {
	assert := assert.New(t)

	cat := NewBaseCatalog()
	if err := cat.LoadDirectory("testdata/catalog"); err != nil {
		t.Fatal(err)
	}

	// Test basic successful validation
	assert.NoError(ValidateProcedureParams(&cat, map[string]any{
		"boolean":     true,
		"integer":     int64(10),
		"stringField": "test",
	}, "example.lexicon.procedure", 0))

	// Test with empty params (all optional in example.lexicon.procedure)
	assert.NoError(ValidateProcedureParams(&cat, map[string]any{}, "example.lexicon.procedure", 0))

	// Test with minimal procedure (no parameters defined)
	assert.NoError(ValidateProcedureParams(&cat, nil, "example.lexicon.minimal.procedure", 0))
	assert.NoError(ValidateProcedureParams(&cat, map[string]any{}, "example.lexicon.minimal.procedure", 0))
}

func TestValidateProcedureParamsErrors(t *testing.T) {
	assert := assert.New(t)

	cat := NewBaseCatalog()
	if err := cat.LoadDirectory("testdata/catalog"); err != nil {
		t.Fatal(err)
	}

	// Wrong schema type (record instead of procedure)
	err := ValidateProcedureParams(&cat, map[string]any{}, "example.lexicon.record", 0)
	assert.Error(err)
	assert.Contains(err.Error(), "not of procedure type")

	// Wrong type for parameter
	err = ValidateProcedureParams(&cat, map[string]any{
		"boolean": "not a boolean",
	}, "example.lexicon.procedure", 0)
	assert.Error(err)
	assert.Contains(err.Error(), "parameter boolean")

	// Minimal procedure with unexpected data
	err = ValidateProcedureParams(&cat, map[string]any{
		"unexpected": "data",
	}, "example.lexicon.minimal.procedure", 0)
	assert.Error(err)
	assert.Contains(err.Error(), "no parameters defined")
}

func TestValidateProcedureInput(t *testing.T) {
	assert := assert.New(t)

	cat := NewBaseCatalog()
	if err := cat.LoadDirectory("testdata/catalog"); err != nil {
		t.Fatal(err)
	}

	// Test minimal procedure with empty object input
	assert.NoError(ValidateProcedureInput(&cat, map[string]any{}, "example.lexicon.minimal.procedure", 0))

	// Test with extra fields (open object allows extra fields)
	assert.NoError(ValidateProcedureInput(&cat, map[string]any{
		"extraField": "value",
	}, "example.lexicon.minimal.procedure", 0))
}

func TestValidateProcedureInputErrors(t *testing.T) {
	assert := assert.New(t)

	cat := NewBaseCatalog()
	if err := cat.LoadDirectory("testdata/catalog"); err != nil {
		t.Fatal(err)
	}

	// Wrong schema type (record instead of procedure)
	err := ValidateProcedureInput(&cat, map[string]any{}, "example.lexicon.record", 0)
	assert.Error(err)
	assert.Contains(err.Error(), "not of procedure type")

	// String instead of object
	err = ValidateProcedureInput(&cat, "not an object", "example.lexicon.minimal.procedure", 0)
	assert.Error(err)
	assert.Contains(err.Error(), "expected an object")

	// Array instead of object
	err = ValidateProcedureInput(&cat, []any{1, 2, 3}, "example.lexicon.minimal.procedure", 0)
	assert.Error(err)
	assert.Contains(err.Error(), "expected an object")
}

func TestInteropQueryParamsValid(t *testing.T) {
	assert := assert.New(t)

	cat := NewBaseCatalog()
	if err := cat.LoadDirectory("testdata/catalog"); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open("testdata/query-params-valid.json")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	jsonBytes, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}

	var fixtures []QueryParamsFixture
	if err := json.Unmarshal(jsonBytes, &fixtures); err != nil {
		t.Fatal(err)
	}

	for _, fixture := range fixtures {
		fmt.Println(fixture.Name)
		d, err := atdata.UnmarshalJSON(fixture.Data)
		if err != nil {
			t.Fatal(err)
		}

		assert.NoError(ValidateQueryParams(&cat, d, "example.lexicon.query", 0))
	}
}

func TestInteropQueryParamsInvalid(t *testing.T) {
	assert := assert.New(t)

	cat := NewBaseCatalog()
	if err := cat.LoadDirectory("testdata/catalog"); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open("testdata/query-params-invalid.json")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	jsonBytes, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}

	var fixtures []QueryParamsFixture
	if err := json.Unmarshal(jsonBytes, &fixtures); err != nil {
		t.Fatal(err)
	}

	for _, fixture := range fixtures {
		fmt.Println(fixture.Name)
		d, err := atdata.UnmarshalJSON(fixture.Data)
		if err != nil {
			t.Fatal(err)
		}
		err = ValidateQueryParams(&cat, d, "example.lexicon.query", 0)
		if err == nil {
			fmt.Println("   FAIL")
		}
		assert.Error(err)
	}
}

func TestInteropProcedureInputValid(t *testing.T) {
	assert := assert.New(t)

	cat := NewBaseCatalog()
	if err := cat.LoadDirectory("testdata/catalog"); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open("testdata/procedure-input-valid.json")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	jsonBytes, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}

	var fixtures []ProcedureInputFixture
	if err := json.Unmarshal(jsonBytes, &fixtures); err != nil {
		t.Fatal(err)
	}

	for _, fixture := range fixtures {
		fmt.Println(fixture.Name)
		d, err := atdata.UnmarshalJSON(fixture.Data)
		if err != nil {
			t.Fatal(err)
		}

		assert.NoError(ValidateProcedureInput(&cat, d, fixture.Ref, 0))
	}
}

func TestInteropProcedureInputInvalid(t *testing.T) {
	assert := assert.New(t)

	cat := NewBaseCatalog()
	if err := cat.LoadDirectory("testdata/catalog"); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open("testdata/procedure-input-invalid.json")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	jsonBytes, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}

	var fixtures []ProcedureInputFixture
	if err := json.Unmarshal(jsonBytes, &fixtures); err != nil {
		t.Fatal(err)
	}

	for _, fixture := range fixtures {
		fmt.Println(fixture.Name)
		// For procedure input invalid tests, we need to handle non-object types
		// that atdata.UnmarshalJSON can't handle. Parse as generic interface{}.
		var d any
		if err := json.Unmarshal(fixture.Data, &d); err != nil {
			t.Fatal(err)
		}
		err = ValidateProcedureInput(&cat, d, fixture.Ref, 0)
		if err == nil {
			fmt.Println("   FAIL")
		}
		assert.Error(err)
	}
}

func TestValidateBody(t *testing.T) {
	assert := assert.New(t)

	cat := NewBaseCatalog()
	if err := cat.LoadDirectory("testdata/catalog"); err != nil {
		t.Fatal(err)
	}

	// Create a simple body schema
	body := SchemaBody{
		Encoding: "application/json",
		Schema: &SchemaDef{
			Inner: SchemaObject{
				Type: "object",
				Properties: map[string]SchemaDef{
					"name": {Inner: SchemaString{Type: "string"}},
					"age":  {Inner: SchemaInteger{Type: "integer"}},
				},
				Required: []string{"name"},
			},
		},
	}

	// Valid data
	assert.NoError(ValidateBody(&cat, body, map[string]any{
		"name": "John",
		"age":  int64(30),
	}, 0))

	// Missing required field
	err := ValidateBody(&cat, body, map[string]any{
		"age": int64(30),
	}, 0)
	assert.Error(err)
	assert.Contains(err.Error(), "required field missing: name")

	// No schema = opaque body (always valid)
	noSchemaBody := SchemaBody{
		Encoding: "application/octet-stream",
	}
	assert.NoError(ValidateBody(&cat, noSchemaBody, "anything goes", 0))
}
