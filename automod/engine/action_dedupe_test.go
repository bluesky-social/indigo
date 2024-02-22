package engine

import (
	"bytes"
	"context"
	"testing"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod/countstore"

	"github.com/stretchr/testify/assert"
)

func alwaysReportAccountRule(c *RecordContext) error {
	c.ReportAccount(ReportReasonOther, "test report")
	return nil
}

func TestAccountReportDedupe(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()
	eng := EngineTestFixture()
	eng.Rules = RuleSet{
		RecordRules: []RecordRuleFunc{
			alwaysReportAccountRule,
		},
	}

	//path := "app.bsky.feed.post/abc123"
	cid1 := syntax.CID("cid123")
	p1 := appbsky.FeedPost{Text: "some post blah"}
	p1buf := new(bytes.Buffer)
	assert.NoError(p1.MarshalCBOR(p1buf))
	p1cbor := p1buf.Bytes()
	id1 := identity.Identity{
		DID:    syntax.DID("did:plc:abc111"),
		Handle: syntax.Handle("handle.example.com"),
	}

	// exact same event multiple times; should only report once
	op := RecordOp{
		Action:     CreateOp,
		DID:        id1.DID,
		Collection: "app.bsky.feed.post",
		RecordKey:  "abc123",
		CID:        &cid1,
		RecordCBOR: p1cbor,
	}
	for i := 0; i < 5; i++ {
		assert.NoError(eng.ProcessRecordOp(ctx, op))
	}

	reports, err := eng.Counters.GetCount(ctx, "automod-quota", "report", countstore.PeriodDay)
	assert.NoError(err)
	assert.Equal(1, reports)
}
