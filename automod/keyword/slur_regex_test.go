package keyword

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSlugContainsExplicitSlur(t *testing.T) {
	assert := assert.New(t)

	fixtures := []struct {
		text     string
		contains string
		is       string
	}{
		{contains: "", is: "", text: ""},
		{contains: "", is: "", text: "hello"},
		{contains: "chink", is: "chink", text: "chink"},
		{contains: "faggot", is: "faggot", text: "faggot"},
		{contains: "faggot", is: "faggot", text: "f4gg0t"},
		{contains: "coon", is: "coon", text: "coon"},
		{contains: "coon", is: "coon", text: "coons"},
		{contains: "", is: "", text: "raccoon"},
		{contains: "", is: "", text: "racoon"},
		{contains: "", is: "", text: "tycoon"},
		{contains: "", is: "", text: "cocoon"},
		{contains: "kike", is: "kike", text: "kike"},
		{contains: "nigger", is: "nigger", text: "nigger"},
		{contains: "nigger", is: "nigger", text: "niggers"},
		{contains: "nigger", is: "nigger", text: "n1gg4"},
		{contains: "nigger", is: "nigger", text: "niggas"},
		{contains: "", is: "", text: "niggle"},
		{contains: "", is: "", text: "niggling"},
		{contains: "", is: "", text: "snigger"},
		{contains: "tranny", is: "tranny", text: "tranny"},
		{contains: "tranny", is: "tranny", text: "trannie"},
		{contains: "tranny", is: "", text: "blahtrannie"},
	}

	for _, fix := range fixtures {
		assert.Equal(fix.contains, SlugContainsExplicitSlur(fix.text))
		assert.Equal(fix.is, SlugIsExplicitSlur(fix.text))
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
		{out: "nigger", text: "pumpkinniggah.bsky.social"},
		{out: "tranny", text: "trannie"},
	}

	for _, fix := range fixtures {
		assert.Equal(fix.out, SlugContainsExplicitSlur(Slugify(fix.text)))
	}
}
