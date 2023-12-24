package rules

import (
	"context"
	"testing"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/automod/engine"

	"github.com/stretchr/testify/assert"
)

func TestBadHashtagPostRule(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()

	eng := engine.EngineTestFixture()
	am1 := automod.AccountMeta{
		Identity: &identity.Identity{
			DID:    syntax.DID("did:plc:abc111"),
			Handle: syntax.Handle("handle.example.com"),
		},
	}
	// path := "app.bsky.feed.post/abc123"
	cid1 := "cid123"
	p1 := appbsky.FeedPost{
		Text: "some post blah",
	}
	op := engine.RecordOp{
		Action:     engine.CreateOp,
		DID:        am1.Identity.DID.String(),
		Collection: "app.bsky.feed.post",
		RecordKey:  "abc123",
		CID:        &cid1,
		Value:      p1,
	}
	evt1 := engine.NewRecordContext(ctx, &eng, am1, op)
	assert.NoError(BadHashtagsPostRule(&evt1, &p1))
	// XXX: test helper to access effects
	//assert.Empty(evt1.effects.RecordFlags)

	p2 := appbsky.FeedPost{
		Text: "some post blah",
		Tags: []string{"one", "slur"},
	}
	op.Value = p2
	evt2 := engine.NewRecordContext(ctx, &eng, am1, op)
	assert.NoError(BadHashtagsPostRule(&evt2, &p2))
	// XXX: helper to access effects
	//assert.NotEmpty(evt2.effects.RecordFlags)
}
