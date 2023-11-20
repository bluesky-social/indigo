package rules

import (
	"testing"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod"

	"github.com/stretchr/testify/assert"
)

func TestMisleadingURLPostRule(t *testing.T) {
	assert := assert.New(t)

	engine := engineFixture()
	am1 := automod.AccountMeta{
		Identity: &identity.Identity{
			DID:    syntax.DID("did:plc:abc111"),
			Handle: syntax.Handle("handle.example.com"),
		},
	}
	path := "app.bsky.feed.post/abc123"
	cid1 := "cid123"
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
	evt1 := engine.NewRecordEvent(am1, path, cid1, &p1)
	assert.NoError(MisleadingURLPostRule(&evt1, &p1))
	assert.NotEmpty(evt1.RecordLabels)
}

func TestMisleadingMentionPostRule(t *testing.T) {
	assert := assert.New(t)

	engine := engineFixture()
	am1 := automod.AccountMeta{
		Identity: &identity.Identity{
			DID:    syntax.DID("did:plc:abc111"),
			Handle: syntax.Handle("handle.example.com"),
		},
	}
	path := "app.bsky.feed.post/abc123"
	cid1 := "cid123"
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
	evt1 := engine.NewRecordEvent(am1, path, cid1, &p1)
	assert.NoError(MisleadingMentionPostRule(&evt1, &p1))
	assert.NotEmpty(evt1.RecordLabels)
}
