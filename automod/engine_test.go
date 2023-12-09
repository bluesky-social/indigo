package automod

import (
	"context"
	"testing"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/stretchr/testify/assert"
)

func TestEngineBasics(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()

	engine := EngineTestFixture()
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
