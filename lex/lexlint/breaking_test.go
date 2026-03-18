package lexlint

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/lexicon"

	"github.com/stretchr/testify/assert"
)

func TestBreakingDefs_InnerTypeChange(t *testing.T) {
	assert := assert.New(t)

	// When a field changes type (e.g. string -> object), breakingDefs should
	// report a type-change error rather than panicking on a type assertion.
	local := lexicon.SchemaDef{Inner: lexicon.SchemaString{}}
	remote := lexicon.SchemaDef{Inner: lexicon.SchemaObject{}}

	issues := breakingDefs("com.example.test", "testField", local, remote)

	assert.Len(issues, 1)
	assert.Equal("type-change", issues[0].LintName)
	assert.Equal("error", issues[0].LintLevel)
}
