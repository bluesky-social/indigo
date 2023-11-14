package rules

import (
	"testing"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/stretchr/testify/assert"
)

func TestBanHashtagPostRule(t *testing.T) {
	assert := assert.New(t)

	engine := engineFixture()
	id1 := identity.Identity{
		DID:    syntax.DID("did:plc:abc111"),
		Handle: syntax.Handle("handle.example.com"),
	}
	rkey := "abc123"
	p1 := appbsky.FeedPost{
		Text: "some post blah",
	}
	evt1 := engine.NewPostEvent(&id1, rkey, &p1)
	assert.NoError(BanHashtagsPostRule(&evt1))
	assert.Empty(evt1.RecordLabels)

	p2 := appbsky.FeedPost{
		Text: "some post blah",
		Tags: []string{"one", "slur"},
	}
	evt2 := engine.NewPostEvent(&id1, rkey, &p2)
	assert.NoError(BanHashtagsPostRule(&evt2))
	assert.NotEmpty(evt2.RecordLabels)
}
