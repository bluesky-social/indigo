package engine

import (
	"bytes"
	"context"
	"testing"

	appgndr "github.com/gander-social/gander-indigo-sovereign/api/gndr"
	"github.com/gander-social/gander-indigo-sovereign/atproto/identity"
	"github.com/gander-social/gander-indigo-sovereign/atproto/syntax"

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
	p1 := appgndr.FeedPost{
		Text: "some post blah",
	}
	p1buf := new(bytes.Buffer)
	assert.NoError(p1.MarshalCBOR(p1buf))
	p1cbor := p1buf.Bytes()

	op := RecordOp{
		Action:     CreateOp,
		DID:        id1.DID,
		Collection: syntax.NSID("gndr.app.feed.post"),
		RecordKey:  syntax.RecordKey("abc123"),
		CID:        &cid1,
		RecordCBOR: p1cbor,
	}
	assert.NoError(eng.ProcessRecordOp(ctx, op))

	p2 := appgndr.FeedPost{
		Text: "some post blah",
		Tags: []string{"one", "slur"},
	}
	p2buf := new(bytes.Buffer)
	assert.NoError(p2.MarshalCBOR(p2buf))
	p2cbor := p2buf.Bytes()
	op.RecordCBOR = p2cbor
	assert.NoError(eng.ProcessRecordOp(ctx, op))
}
