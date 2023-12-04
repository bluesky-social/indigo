package automod

import (
	"context"
	"log/slog"
	"testing"
	"time"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/stretchr/testify/assert"
)

func simpleRule(evt *RecordEvent, post *appbsky.FeedPost) error {
	for _, tag := range post.Tags {
		if evt.InSet("bad-hashtags", tag) {
			evt.AddRecordLabel("bad-hashtag")
			break
		}
	}
	for _, facet := range post.Facets {
		for _, feat := range facet.Features {
			if feat.RichtextFacet_Tag != nil {
				tag := feat.RichtextFacet_Tag.Tag
				if evt.InSet("bad-hashtags", tag) {
					evt.AddRecordLabel("bad-hashtag")
					break
				}
			}
		}
	}
	return nil
}

func engineFixture() Engine {
	rules := RuleSet{
		PostRules: []PostRuleFunc{
			simpleRule,
		},
	}
	cache := NewMemCacheStore(10, time.Hour)
	flags := NewMemFlagStore()
	sets := NewMemSetStore()
	sets.Sets["bad-hashtags"] = make(map[string]bool)
	sets.Sets["bad-hashtags"]["slur"] = true
	dir := identity.NewMockDirectory()
	id1 := identity.Identity{
		DID:    syntax.DID("did:plc:abc111"),
		Handle: syntax.Handle("handle.example.com"),
	}
	dir.Insert(id1)
	engine := Engine{
		Logger:    slog.Default(),
		Directory: &dir,
		Counters:  NewMemCountStore(),
		Sets:      sets,
		Flags:     flags,
		Cache:     cache,
		Rules:     rules,
	}
	return engine
}

func TestEngineBasics(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()

	engine := engineFixture()
	id1 := identity.Identity{
		DID:    syntax.DID("did:plc:abc111"),
		Handle: syntax.Handle("handle.example.com"),
	}
	path := "app.bsky.feed.post/abc123"
	cid1 := "cid123"
	p1 := appbsky.FeedPost{
		Text: "some post blah",
	}
	assert.NoError(engine.ProcessRecord(ctx, id1.DID, path, cid1, &p1))

	p2 := appbsky.FeedPost{
		Text: "some post blah",
		Tags: []string{"one", "slur"},
	}
	assert.NoError(engine.ProcessRecord(ctx, id1.DID, path, cid1, &p2))
}
