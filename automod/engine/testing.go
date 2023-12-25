package engine

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
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

func MustLoadCapture(capPath string) AccountCapture {
	f, err := os.Open(capPath)
	if err != nil {
		panic(err)
	}
	defer func() { _ = f.Close() }()

	raw, err := io.ReadAll(f)
	if err != nil {
		panic(err)
	}

	var capture AccountCapture
	if err := json.Unmarshal(raw, &capture); err != nil {
		panic(err)
	}
	return capture
}

// Test helper which processes all the records from a capture. Intentionally exported, for use in other packages.
//
// This method replaces any pre-existing directory on the engine with a mock directory.
func ProcessCaptureRules(eng *Engine, capture AccountCapture) error {
	ctx := context.Background()

	did := capture.AccountMeta.Identity.DID
	dir := identity.NewMockDirectory()
	dir.Insert(*capture.AccountMeta.Identity)
	eng.Directory = &dir

	// initial identity rules
	eng.ProcessIdentityEvent(ctx, "new", did)

	// all the post rules
	for _, pr := range capture.PostRecords {
		aturi, err := syntax.ParseATURI(pr.Uri)
		if err != nil {
			return err
		}
		did, err := aturi.Authority().AsDID()
		if err != nil {
			return err
		}
		recCID := syntax.CID(pr.Cid)
		eng.Logger.Debug("processing record", "did", did)
		op := RecordOp{
			Action:     CreateOp,
			DID:        did,
			Collection: aturi.Collection(),
			RecordKey:  aturi.RecordKey(),
			CID:        &recCID,
			Value:      pr.Value.Val,
		}
		eng.ProcessRecordOp(ctx, op)
	}
	return nil
}

// Helper to access the private effects field from a context. Intended for use in test code, *not* from rules.
func ExtractEffects(c *BaseContext) Effects {
	return c.effects
}
