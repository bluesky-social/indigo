package engine

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

	eng := EngineTestFixture()
	id1 := identity.Identity{
		DID:    syntax.DID("did:plc:abc111"),
		Handle: syntax.Handle("handle.example.com"),
	}
	cid1 := syntax.CID("cid123")
	p1 := appbsky.FeedPost{
		Text: "some post blah",
	}
	op := RecordOp{
		Action:     CreateOp,
		DID:        id1.DID,
		Collection: syntax.NSID("app.bsky.feed.post"),
		RecordKey:  syntax.RecordKey("abc123"),
		CID:        &cid1,
		Value:      &p1,
	}
	assert.NoError(eng.ProcessRecordOp(ctx, op))

	p2 := appbsky.FeedPost{
		Text: "some post blah",
		Tags: []string{"one", "slur"},
	}
	op.Value = &p2
	assert.NoError(eng.ProcessRecordOp(ctx, op))
}
