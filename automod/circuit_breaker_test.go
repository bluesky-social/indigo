package automod

import (
	"context"
	"fmt"
	"testing"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/stretchr/testify/assert"
)

func alwaysTakedownRecordRule(evt *RecordEvent) error {
	evt.TakedownRecord()
	return nil
}

func alwaysReportRecordRule(evt *RecordEvent) error {
	evt.ReportRecord(ReportReasonOther, "test report")
	return nil
}

func TestTakedownCircuitBreaker(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()
	engine := engineFixture()
	dir := identity.NewMockDirectory()
	engine.Directory = &dir
	// note that this is a record-level action, not account-level
	engine.Rules = RuleSet{
		RecordRules: []RecordRuleFunc{
			alwaysTakedownRecordRule,
		},
	}

	path := "app.bsky.feed.post/abc123"
	cid1 := "cid123"
	p1 := appbsky.FeedPost{Text: "some post blah"}

	// generate double the quote of events; expect to only count the quote worth of actions
	for i := 0; i < 2*QuotaModTakedownDay; i++ {
		ident := identity.Identity{
			DID:    syntax.DID(fmt.Sprintf("did:plc:abc%d", i)),
			Handle: syntax.Handle("handle.example.com"),
		}
		dir.Insert(ident)
		assert.NoError(engine.ProcessRecord(ctx, ident.DID, path, cid1, &p1))
	}

	takedowns, err := engine.GetCount("automod-quota", "takedown", PeriodDay)
	assert.NoError(err)
	assert.Equal(QuotaModTakedownDay, takedowns)

	reports, err := engine.GetCount("automod-quota", "report", PeriodDay)
	assert.NoError(err)
	assert.Equal(0, reports)
}

func TestReportCircuitBreaker(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()
	engine := engineFixture()
	dir := identity.NewMockDirectory()
	engine.Directory = &dir
	engine.Rules = RuleSet{
		RecordRules: []RecordRuleFunc{
			alwaysReportRecordRule,
		},
	}

	path := "app.bsky.feed.post/abc123"
	cid1 := "cid123"
	p1 := appbsky.FeedPost{Text: "some post blah"}

	// generate double the quota of events; expect to only count the quota worth of actions
	for i := 0; i < 2*QuotaModReportDay; i++ {
		ident := identity.Identity{
			DID:    syntax.DID(fmt.Sprintf("did:plc:abc%d", i)),
			Handle: syntax.Handle("handle.example.com"),
		}
		dir.Insert(ident)
		assert.NoError(engine.ProcessRecord(ctx, ident.DID, path, cid1, &p1))
	}

	takedowns, err := engine.GetCount("automod-quota", "takedown", PeriodDay)
	assert.NoError(err)
	assert.Equal(0, takedowns)

	reports, err := engine.GetCount("automod-quota", "report", PeriodDay)
	assert.NoError(err)
	assert.Equal(QuotaModReportDay, reports)
}
