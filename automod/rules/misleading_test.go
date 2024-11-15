package rules

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/automod/engine"
	"github.com/bluesky-social/indigo/automod/helpers"

	"github.com/stretchr/testify/assert"
)

func TestMisleadingURLPostRule(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()

	eng := engine.EngineTestFixture()
	am1 := automod.AccountMeta{
		Identity: &identity.Identity{
			DID:    syntax.DID("did:plc:abc111"),
			Handle: syntax.Handle("handle.example.com"),
		},
	}
	cid1 := syntax.CID("cid123")
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
	p1buf := new(bytes.Buffer)
	assert.NoError(p1.MarshalCBOR(p1buf))
	p1cbor := p1buf.Bytes()
	op := engine.RecordOp{
		Action:     engine.CreateOp,
		DID:        am1.Identity.DID,
		Collection: syntax.NSID("app.bsky.feed.post"),
		RecordKey:  syntax.RecordKey("abc123"),
		CID:        &cid1,
		RecordCBOR: p1cbor,
	}
	c1 := engine.NewRecordContext(ctx, &eng, am1, op)
	assert.NoError(MisleadingURLPostRule(&c1, &p1))
	eff1 := engine.ExtractEffects(&c1.BaseContext)
	assert.NotEmpty(eff1.RecordFlags)
}

func TestMisleadingMentionPostRule(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()

	eng := engine.EngineTestFixture()
	am1 := automod.AccountMeta{
		Identity: &identity.Identity{
			DID:    syntax.DID("did:plc:abc111"),
			Handle: syntax.Handle("handle.example.com"),
		},
	}
	cid1 := syntax.CID("cid123")
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
	p1buf := new(bytes.Buffer)
	assert.NoError(p1.MarshalCBOR(p1buf))
	p1cbor := p1buf.Bytes()
	op := engine.RecordOp{
		Action:     engine.CreateOp,
		DID:        am1.Identity.DID,
		Collection: syntax.NSID("app.bsky.feed.post"),
		RecordKey:  syntax.RecordKey("abc123"),
		CID:        &cid1,
		RecordCBOR: p1cbor,
	}
	c1 := engine.NewRecordContext(ctx, &eng, am1, op)
	assert.NoError(MisleadingMentionPostRule(&c1, &p1))
	eff1 := engine.ExtractEffects(&c1.BaseContext)
	assert.NotEmpty(eff1.RecordFlags)
}

func pstr(raw string) *string {
	return &raw
}

func TestIsMisleadingURL(t *testing.T) {
	assert := assert.New(t)
	logger := slog.Default()

	fixtures := []struct {
		facet helpers.PostFacet
		out   bool
	}{
		{
			facet: helpers.PostFacet{
				Text: "https://atproto.com",
				URL:  pstr("https://atproto.com"),
			},
			out: false,
		},
		{
			facet: helpers.PostFacet{
				Text: "https://atproto.com",
				URL:  pstr("https://evil.com"),
			},
			out: true,
		},
		{
			facet: helpers.PostFacet{
				Text: "https://www.atproto.com",
				URL:  pstr("https://atproto.com"),
			},
			out: false,
		},
		{
			facet: helpers.PostFacet{
				Text: "https://atproto.com",
				URL:  pstr("https://www.atproto.com"),
			},
			out: false,
		},
		{
			facet: helpers.PostFacet{
				Text: "[example.com]",
				URL:  pstr("https://www.example.com"),
			},
			out: false,
		},
		{
			facet: helpers.PostFacet{
				Text: "example.com...",
				URL:  pstr("https://example.com.evil.com"),
			},
			out: true,
		},
		{
			facet: helpers.PostFacet{
				Text: "ATPROTO.com...",
				URL:  pstr("https://atproto.com"),
			},
			out: false,
		},
		{
			facet: helpers.PostFacet{
				Text: "1234.5678",
				URL:  pstr("https://arxiv.org/abs/1234.5678"),
			},
			out: false,
		},
		{
			facet: helpers.PostFacet{
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
