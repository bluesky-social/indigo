package rules

import (
	"log/slog"
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
	assert.NotEmpty(evt1.RecordFlags)
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
	assert.NotEmpty(evt1.RecordFlags)
}

func pstr(raw string) *string {
	return &raw
}

func TestIsMisleadingURL(t *testing.T) {
	assert := assert.New(t)
	logger := slog.Default()

	fixtures := []struct {
		facet PostFacet
		out   bool
	}{
		{
			facet: PostFacet{
				Text: "https://atproto.com",
				URL:  pstr("https://atproto.com"),
			},
			out: false,
		},
		{
			facet: PostFacet{
				Text: "https://atproto.com",
				URL:  pstr("https://evil.com"),
			},
			out: true,
		},
		{
			facet: PostFacet{
				Text: "https://www.atproto.com",
				URL:  pstr("https://atproto.com"),
			},
			out: false,
		},
		{
			facet: PostFacet{
				Text: "https://atproto.com",
				URL:  pstr("https://www.atproto.com"),
			},
			out: false,
		},
		{
			facet: PostFacet{
				Text: "[example.com]",
				URL:  pstr("https://www.example.com"),
			},
			out: false,
		},
		{
			facet: PostFacet{
				Text: "example.com...",
				URL:  pstr("https://example.com.evil.com"),
			},
			out: true,
		},
		{
			facet: PostFacet{
				Text: "ATPROTO.com...",
				URL:  pstr("https://atproto.com"),
			},
			out: false,
		},
		{
			facet: PostFacet{
				Text: "1234.5678",
				URL:  pstr("https://arxiv.org/abs/1234.5678"),
			},
			out: false,
		},
		{
			facet: PostFacet{
				Text: "www.techdirt.com…",
				URL:  pstr("https://www.techdirt.com/"),
			},
			out: false,
		},
	}

	for _, fix := range fixtures {
		assert.Equal(fix.out, isMisleadingURLFacet(fix.facet, logger))
	}
}
