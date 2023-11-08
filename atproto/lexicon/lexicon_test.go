package lexicon

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBasicCatalog(t *testing.T) {
	assert := assert.New(t)

	cat := NewCatalog()
	if err := cat.LoadDirectory("testdata/catalog"); err != nil {
		t.Fatal(err)
	}

	def, err := cat.Resolve("com.atproto.label.defs#label")
	if err != nil {
		t.Fatal(err)
	}
	assert.NoError(cat.validateData(
		def.Def,
		map[string]any{
			"cid": "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi",
			"cts": "2000-01-01T00:00:00.000Z",
			"neg": false,
			"src": "did:example:labeler",
			"uri": "at://did:plc:asdf123/com.atproto.feed.post/asdf123",
			"val": "test-label",
		},
	))

	assert.Error(cat.validateData(
		def.Def,
		map[string]any{
			"cid": "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi",
			"cts": "2000-01-01T00:00:00.000Z",
			"neg": false,
			"uri": "at://did:plc:asdf123/com.atproto.feed.post/asdf123",
			"val": "test-label",
		},
	))
}
