package keyword

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSlugContainsExplicitSlur(t *testing.T) {
	assert := assert.New(t)

	fixtures := []struct {
		text string
		out  string
	}{
		{out: "", text: ""},
		{out: "", text: "hello"},
		{out: "chink", text: "chink"},
		{out: "faggot", text: "faggot"},
		{out: "faggot", text: "f4gg0t"},
		{out: "coon", text: "coon"},
		{out: "coon", text: "coons"},
		{out: "", text: "raccoon"},
		{out: "", text: "racoon"},
		{out: "", text: "tycoon"},
		{out: "", text: "cocoon"},
		{out: "kike", text: "kike"},
		{out: "nigger", text: "nigger"},
		{out: "nigger", text: "niggers"},
		{out: "nigger", text: "n1gg4"},
		{out: "nigger", text: "niggas"},
		{out: "", text: "niggle"},
		{out: "", text: "niggling"},
		{out: "", text: "snigger"},
		{out: "tranny", text: "tranny"},
		{out: "tranny", text: "trannie"},
	}

	for _, fix := range fixtures {
		assert.Equal(fix.out, SlugContainsExplicitSlur(fix.text))
	}
}

func TestStringContainsExplicitSlur(t *testing.T) {
	assert := assert.New(t)

	fixtures := []struct {
		text string
		out  string
	}{
		{out: "", text: ""},
		{out: "", text: "hello"},
		{out: "chink", text: "CHINK"},
		{out: "faggot", text: "f-a-g-g-o-t"},
		{out: "faggot", text: "f a g g o t"},
		{out: "faggot", text: "f\na\ng\ng\no\nt"},
		{out: "kike", text: "kike"},
		{out: "nigger", text: "niggers"},
		{out: "nigger", text: "niggers.bsky.social"},
		{out: "tranny", text: "trannie"},
	}

	for _, fix := range fixtures {
		assert.Equal(fix.out, SlugContainsExplicitSlur(Slugify(fix.text)))
	}
}
