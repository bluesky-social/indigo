package keyword

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIdentifierContainsExplicitKeyword(t *testing.T) {
	assert := assert.New(t)

	fixtures := []struct {
		text string
		out  bool
	}{
		{out: false, text: ""},
		{out: false, text: "hello"},
		{out: true, text: "chink"},
		{out: true, text: "coon"},
		{out: true, text: "faggot"},
		{out: true, text: "kike"},
		{out: true, text: "nigger"},
		{out: true, text: "tranny"},
		{out: true, text: "faggot.bsky.social"},
		{out: true, text: "fagg.ot"},
		{out: true, text: "niggers.bsky.social"},
		{out: true, text: "n.igger.bsky.social"},
		{out: true, text: "n-igger.bsky.social"},
		{out: true, text: "n_igger.bsky.social"},
	}

	for _, fix := range fixtures {
		assert.Equal(fix.out, IdentifierContainsExplicitKeyword(fix.text))
	}
}
