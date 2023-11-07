package lexicon

import (
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBasicLabelLexicon(t *testing.T) {
	assert := assert.New(t)

	f, err := os.Open("testdata/valid/com_atproto_label_defs.json")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	jsonBytes, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}

	var schema SchemaFile
	if err := json.Unmarshal(jsonBytes, &schema); err != nil {
		t.Fatal(err)
	}

	outBytes, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}

	var beforeMap map[string]any
	if err := json.Unmarshal(jsonBytes, &beforeMap); err != nil {
		t.Fatal(err)
	}

	var afterMap map[string]any
	if err := json.Unmarshal(outBytes, &afterMap); err != nil {
		t.Fatal(err)
	}

	assert.Equal(beforeMap, afterMap)
}
