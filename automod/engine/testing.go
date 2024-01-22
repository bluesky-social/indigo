package engine

import (
	"log/slog"
	"time"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod/cachestore"
	"github.com/bluesky-social/indigo/automod/countstore"
	"github.com/bluesky-social/indigo/automod/flagstore"
	"github.com/bluesky-social/indigo/automod/setstore"
)

var _ PostRuleFunc = simpleRule

func simpleRule(c *RecordContext, post *appbsky.FeedPost) error {
	for _, tag := range post.Tags {
		if c.InSet("bad-hashtags", tag) {
			c.AddRecordLabel("bad-hashtag")
			break
		}
	}
	for _, facet := range post.Facets {
		for _, feat := range facet.Features {
			if feat.RichtextFacet_Tag != nil {
				tag := feat.RichtextFacet_Tag.Tag
				if c.InSet("bad-hashtags", tag) {
					c.AddRecordLabel("bad-hashtag")
					break
				}
			}
		}
	}
	return nil
}

func EngineTestFixture() Engine {
	rules := RuleSet{
		PostRules: []PostRuleFunc{
			simpleRule,
		},
	}
	cache := cachestore.NewMemCacheStore(10, time.Hour)
	flags := flagstore.NewMemFlagStore()
	sets := setstore.NewMemSetStore()
	sets.Sets["bad-hashtags"] = make(map[string]bool)
	sets.Sets["bad-hashtags"]["slur"] = true
	sets.Sets["bad-words"] = make(map[string]bool)
	sets.Sets["bad-words"]["hardr"] = true
	sets.Sets["bad-words"]["hardestr"] = true
	sets.Sets["worst-words"] = make(map[string]bool)
	sets.Sets["worst-words"]["hardestr"] = true
	dir := identity.NewMockDirectory()
	id1 := identity.Identity{
		DID:    syntax.DID("did:plc:abc111"),
		Handle: syntax.Handle("handle.example.com"),
	}
	dir.Insert(id1)
	eng := Engine{
		Logger:    slog.Default(),
		Directory: &dir,
		Counters:  countstore.NewMemCountStore(),
		Sets:      sets,
		Flags:     flags,
		Cache:     cache,
		Rules:     rules,
	}
	return eng
}

// Helper to access the private effects field from a context. Intended for use in test code, *not* from rules.
func ExtractEffects(c *BaseContext) *Effects {
	return c.effects
}
