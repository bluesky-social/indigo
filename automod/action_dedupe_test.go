package automod

import (
	"context"
	"testing"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod/countstore"

	"github.com/stretchr/testify/assert"
)

func alwaysReportAccountRule(evt *RecordEvent) error {
	evt.ReportAccount(ReportReasonOther, "test report")
	return nil
}

func TestAccountReportDedupe(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()
	engine := EngineTestFixture()
	engine.Rules = RuleSet{
		RecordRules: []RecordRuleFunc{
			alwaysReportAccountRule,
		},
	}

	path := "app.bsky.feed.post/abc123"
	cid1 := "cid123"
	p1 := appbsky.FeedPost{Text: "some post blah"}
	id1 := identity.Identity{
		DID:    syntax.DID("did:plc:abc111"),
		Handle: syntax.Handle("handle.example.com"),
	}

	// exact same event multiple times; should only report once
	for i := 0; i < 5; i++ {
		assert.NoError(engine.ProcessRecord(ctx, id1.DID, path, cid1, &p1))
	}

	reports, err := engine.GetCount("automod-quota", "report", countstore.PeriodDay)
	assert.NoError(err)
	assert.Equal(1, reports)
}
