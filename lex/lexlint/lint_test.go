package lexlint

import (
	"encoding/json"
	"io"
	"os"
	"sort"
	"testing"

	"github.com/bluesky-social/indigo/atproto/lexicon"

	"github.com/stretchr/testify/assert"
)

type LintFixture struct {
	Name   string             `json:"name"`
	Schema lexicon.SchemaFile `json:"schema"`
	Issues []string           `json:"issues"`
}

func TestLexLintFixtures(t *testing.T) {
	assert := assert.New(t)

	f, err := os.Open("testdata/lint-examples.json")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	jsonBytes, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}

	var fixtures []LintFixture
	if err := json.Unmarshal(jsonBytes, &fixtures); err != nil {
		t.Fatal(err)
	}

	for _, f := range fixtures {
		assert.NoError(f.Schema.FinishParse())

		issues := LintSchemaFile(&f.Schema)

		found := []string{}
		for _, iss := range issues {
			found = append(found, iss.LintName)
		}

		sort.Strings(found)
		sort.Strings(f.Issues)
		assert.Equal(f.Issues, found, f.Name)
	}
}
