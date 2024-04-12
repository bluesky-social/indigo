package search

import (
	"context"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/stretchr/testify/assert"
)

func TestParseQuery(t *testing.T) {
	ctx := context.Background()
	assert := assert.New(t)
	dir := identity.NewMockDirectory()
	ident := identity.Identity{
		Handle: syntax.Handle("known.example.com"),
		DID:    syntax.DID("did:plc:abc222"),
	}
	dir.Insert(ident)

	var p PostSearchParams

	p = ParsePostQuery(ctx, &dir, "", nil)
	assert.Equal("*", p.Query)
	assert.Empty(p.Filters())

	q1 := "some +test \"with phrase\" -ok"
	p = ParsePostQuery(ctx, &dir, q1, nil)
	assert.Equal(q1, p.Query)
	assert.Empty(p.Filters())

	q2 := "missing from:missing.example.com"
	p = ParsePostQuery(ctx, &dir, q2, nil)
	assert.Equal("missing", p.Query)
	assert.Empty(p.Filters())

	q3 := "known from:known.example.com"
	p = ParsePostQuery(ctx, &dir, q3, nil)
	assert.Equal("known", p.Query)
	assert.NotNil(p.Author)
	if p.Author != nil {
		assert.Equal("did:plc:abc222", p.Author.String())
	}

	q4 := "from:known.example.com"
	p = ParsePostQuery(ctx, &dir, q4, nil)
	assert.Equal("*", p.Query)
	assert.Equal(1, len(p.Filters()))

	q5 := `from:known.example.com "multi word phrase" coolio blorg`
	p = ParsePostQuery(ctx, &dir, q5, nil)
	assert.Equal(`"multi word phrase" coolio blorg`, p.Query)
	assert.NotNil(p.Author)
	if p.Author != nil {
		assert.Equal("did:plc:abc222", p.Author.String())
	}
	assert.Equal(1, len(p.Filters()))

	q6 := `from:known.example.com #cool_tag some other stuff`
	p = ParsePostQuery(ctx, &dir, q6, nil)
	assert.Equal(`some other stuff`, p.Query)
	assert.NotNil(p.Author)
	if p.Author != nil {
		assert.Equal("did:plc:abc222", p.Author.String())
	}
	assert.Equal([]string{"cool_tag"}, p.Tags)
	assert.Equal(2, len(p.Filters()))

	q7 := "known from:@known.example.com"
	p = ParsePostQuery(ctx, &dir, q7, nil)
	assert.Equal("known", p.Query)
	assert.NotNil(p.Author)
	if p.Author != nil {
		assert.Equal("did:plc:abc222", p.Author.String())
	}
	assert.Equal(1, len(p.Filters()))

	q8 := "known from:me"
	p = ParsePostQuery(ctx, &dir, q8, &ident.DID)
	assert.Equal("known", p.Query)
	assert.NotNil(p.Author)
	if p.Author != nil {
		assert.Equal("did:plc:abc222", p.Author.String())
	}
	assert.Equal(1, len(p.Filters()))

	q9 := "did:plc:abc222"
	p = ParsePostQuery(ctx, &dir, q9, nil)
	assert.Equal("*", p.Query)
	assert.Equal(1, len(p.Filters()))
	if p.Author != nil {
		assert.Equal("did:plc:abc222", p.Author.String())
	}

	// TODO: more parsing tests: bare handles, to:, since:, until:, URL, domain:, lang
}
