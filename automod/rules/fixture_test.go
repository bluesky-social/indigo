package rules

import (
	"log/slog"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/xrpc"
)

func engineFixture() automod.Engine {
	rules := automod.RuleSet{
		PostRules: []automod.PostRuleFunc{
			BadHashtagsPostRule,
		},
	}
	flags := automod.NewMemFlagStore()
	cache := automod.NewMemCacheStore(10, time.Hour)
	sets := automod.NewMemSetStore()
	sets.Sets["bad-hashtags"] = make(map[string]bool)
	sets.Sets["bad-hashtags"]["slur"] = true
	dir := identity.NewMockDirectory()
	id1 := identity.Identity{
		DID:    syntax.DID("did:plc:abc111"),
		Handle: syntax.Handle("handle.example.com"),
	}
	id2 := identity.Identity{
		DID:    syntax.DID("did:plc:abc222"),
		Handle: syntax.Handle("imposter.example.com"),
	}
	dir.Insert(id1)
	dir.Insert(id2)
	adminc := xrpc.Client{
		Host: "http://dummy.local",
	}
	engine := automod.Engine{
		Logger:      slog.Default(),
		Directory:   &dir,
		Counters:    automod.NewMemCountStore(),
		Sets:        sets,
		Flags:       flags,
		Cache:       cache,
		Rules:       rules,
		AdminClient: &adminc,
	}
	return engine
}
