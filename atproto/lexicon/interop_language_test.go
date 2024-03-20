package lexicon

import (
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

type LexiconFixture struct {
	Name    string          `json:"name"`
	Lexicon json.RawMessage `json:"lexicon"`
}

func TestInteropLexiconValid(t *testing.T) {

	f, err := os.Open("testdata/lexicon-valid.json")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	jsonBytes, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}

	var fixtures []LexiconFixture
	if err := json.Unmarshal(jsonBytes, &fixtures); err != nil {
		t.Fatal(err)
	}

	for _, f := range fixtures {
		testLexiconFixtureValid(t, f)
	}
}

func testLexiconFixtureValid(t *testing.T, fixture LexiconFixture) {
	assert := assert.New(t)

	var schema SchemaFile
	if err := json.Unmarshal(fixture.Lexicon, &schema); err != nil {
		t.Fatal(err)
	}

	outBytes, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}

	var beforeMap map[string]any
	if err := json.Unmarshal(fixture.Lexicon, &beforeMap); err != nil {
		t.Fatal(err)
	}

	var afterMap map[string]any
	if err := json.Unmarshal(outBytes, &afterMap); err != nil {
		t.Fatal(err)
	}

	assert.Equal(beforeMap, afterMap)
}

func TestInteropLexiconInvalid(t *testing.T) {

	f, err := os.Open("testdata/lexicon-invalid.json")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	jsonBytes, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}

	var fixtures []LexiconFixture
	if err := json.Unmarshal(jsonBytes, &fixtures); err != nil {
		t.Fatal(err)
	}

	for _, f := range fixtures {
		testLexiconFixtureInvalid(t, f)
	}
}

func testLexiconFixtureInvalid(t *testing.T, fixture LexiconFixture) {
	assert := assert.New(t)

	var schema SchemaFile
	err := json.Unmarshal(fixture.Lexicon, &schema)
	assert.Error(err)
}
