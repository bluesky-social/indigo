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

func TestBadWordHandleRule(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()

	eng := engine.EngineTestFixture()
	am1 := automod.AccountMeta{
		Identity: &identity.Identity{
			DID:    syntax.DID("did:plc:abc111"),
			Handle: syntax.Handle("handle.example.com"),
		},
	}
	am2 := automod.AccountMeta{
		Identity: &identity.Identity{
			DID:    syntax.DID("did:plc:abc222"),
			Handle: syntax.Handle("hardr.example.com"),
		},
	}
	am3 := automod.AccountMeta{
		Identity: &identity.Identity{
			DID:    syntax.DID("did:plc:abc333"),
			Handle: syntax.Handle("f.agg.ot"),
		},
	}

	ac1 := engine.NewAccountContext(ctx, &eng, am1)
	assert.NoError(BadWordHandleRule(&ac1))
	eff1 := engine.ExtractEffects(&ac1.BaseContext)
	assert.Empty(eff1.RecordFlags)

	ac2 := engine.NewAccountContext(ctx, &eng, am2)
	assert.NoError(BadWordHandleRule(&ac2))
	eff2 := engine.ExtractEffects(&ac2.BaseContext)
	assert.Equal([]string{"bad-word-handle"}, eff2.AccountFlags)

	ac3 := engine.NewAccountContext(ctx, &eng, am3)
	assert.NoError(BadWordHandleRule(&ac3))
	eff3 := engine.ExtractEffects(&ac3.BaseContext)
	assert.Equal([]string{"bad-word-handle"}, eff3.AccountFlags)
}

func TestBadWordPostRule(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()

	eng := engine.EngineTestFixture()
	am1 := automod.AccountMeta{
		Identity: &identity.Identity{
			DID:    syntax.DID("did:plc:abc111"),
			Handle: syntax.Handle("handle.example.com"),
		},
	}

	// record key
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
		RecordKey:  syntax.RecordKey("fagg0t"),
		CID:        &cid1,
		RecordCBOR: p1cbor,
	}
	c1 := engine.NewRecordContext(ctx, &eng, am1, op)
	assert.NoError(BadWordRecordKeyRule(&c1))
	eff1 := engine.ExtractEffects(&c1.BaseContext)
	assert.Equal([]string{"bad-word-recordkey"}, eff1.RecordFlags)

	// token in body
	p2 := appbsky.FeedPost{
		Text: "some post hardestr blah",
	}
	p2buf := new(bytes.Buffer)
	assert.NoError(p2.MarshalCBOR(p2buf))
	p2cbor := p2buf.Bytes()
	op2 := engine.RecordOp{
		Action:     engine.CreateOp,
		DID:        am1.Identity.DID,
		Collection: syntax.NSID("app.bsky.feed.post"),
		RecordKey:  syntax.RecordKey("abc123"),
		CID:        &cid1,
		RecordCBOR: p2cbor,
	}
	c2 := engine.NewRecordContext(ctx, &eng, am1, op2)
	assert.NoError(BadWordPostRule(&c2, &p2))
	eff2 := engine.ExtractEffects(&c2.BaseContext)
	assert.Equal([]string{"bad-word-text"}, eff2.RecordFlags)
}
