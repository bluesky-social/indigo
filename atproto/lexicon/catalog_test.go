package lexicon

import (
	"embed"
	"testing"

	"github.com/stretchr/testify/assert"
)

//go:embed testdata/catalog
var embedDir embed.FS

func TestEmbedCatalog(t *testing.T) {
	assert := assert.New(t)

	cat := NewBaseCatalog()

	err := cat.LoadEmbedFS(embedDir)
	assert.NoError(err)

	_, err = cat.Resolve("example.lexicon.query")
	assert.NoError(err)

	_, err = cat.Resolve("example.lexicon.notThere")
	assert.Error(err)
}

func TestDirCatalog(t *testing.T) {
	assert := assert.New(t)

	cat := NewBaseCatalog()

	err := cat.LoadDirectory("testdata/catalog")
	assert.NoError(err)

	_, err = cat.Resolve("example.lexicon.query")
	assert.NoError(err)

	_, err = cat.Resolve("example.lexicon.notThere")
	assert.Error(err)
}
