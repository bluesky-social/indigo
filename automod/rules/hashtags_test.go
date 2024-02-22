package rules

import (
	"bytes"
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
	cid1 := syntax.CID("cid123")
	p1 := appbsky.FeedPost{
		Text: "some post blah",
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
	assert.NoError(BadHashtagsPostRule(&c1, &p1))
	eff1 := engine.ExtractEffects(&c1.BaseContext)
	assert.Empty(eff1.RecordFlags)

	p2 := appbsky.FeedPost{
		Text: "some post blah",
		Tags: []string{"one", "slur"},
	}
	p2buf := new(bytes.Buffer)
	assert.NoError(p2.MarshalCBOR(p2buf))
	p2cbor := p2buf.Bytes()
	op.RecordCBOR = p2cbor
	c2 := engine.NewRecordContext(ctx, &eng, am1, op)
	assert.NoError(BadHashtagsPostRule(&c2, &p2))
	eff2 := engine.ExtractEffects(&c2.BaseContext)
	assert.NotEmpty(eff2.RecordFlags)
}
