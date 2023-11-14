package rules

import (
	"testing"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/stretchr/testify/assert"
)

func TestMisleadingURLPostRule(t *testing.T) {
	assert := assert.New(t)

	engine := engineFixture()
	id1 := identity.Identity{
		DID:    syntax.DID("did:plc:abc111"),
		Handle: syntax.Handle("handle.example.com"),
	}
	rkey := "abc123"
	p1 := appbsky.FeedPost{
		Text: "https://safe.com/ is very reputable",
		Facets: []*appbsky.RichtextFacet{
			&appbsky.RichtextFacet{
				Features: []*appbsky.RichtextFacet_Features_Elem{
					&appbsky.RichtextFacet_Features_Elem{
						RichtextFacet_Link: &appbsky.RichtextFacet_Link{
							Uri: "https://evil.com",
						},
					},
				},
				Index: &appbsky.RichtextFacet_ByteSlice{
					ByteStart: 0,
					ByteEnd:   16,
				},
			},
		},
	}
	evt1 := engine.NewPostEvent(&id1, rkey, &p1)
	assert.NoError(MisleadingURLPostRule(&evt1))
	assert.NotEmpty(evt1.RecordLabels)
}

func TestMisleadingMentionPostRule(t *testing.T) {
	assert := assert.New(t)

	engine := engineFixture()
	id1 := identity.Identity{
		DID:    syntax.DID("did:plc:abc111"),
		Handle: syntax.Handle("handle.example.com"),
	}
	rkey := "abc123"
	p1 := appbsky.FeedPost{
		Text: "@handle.example.com is a friend",
		Facets: []*appbsky.RichtextFacet{
			&appbsky.RichtextFacet{
				Features: []*appbsky.RichtextFacet_Features_Elem{
					&appbsky.RichtextFacet_Features_Elem{
						RichtextFacet_Mention: &appbsky.RichtextFacet_Mention{
							Did: "did:plc:abc222",
						},
					},
				},
				Index: &appbsky.RichtextFacet_ByteSlice{
					ByteStart: 1,
					ByteEnd:   19,
				},
			},
		},
	}
	evt1 := engine.NewPostEvent(&id1, rkey, &p1)
	assert.NoError(MisleadingMentionPostRule(&evt1))
	assert.NotEmpty(evt1.RecordLabels)
}
