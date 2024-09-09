package rules

import (
	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/automod/countstore"
)

var _ automod.RecordRuleFunc = ResolveAppealOnRecordDeleteRule

func ResolveAppealOnRecordDeleteRule(c *automod.RecordContext) error {
	switch c.RecordOp.Collection {
	case "app.bsky.feed.post":
		hasAppeal := c.GetCount("appeal", c.RecordOp.ATURI().String(), countstore.PeriodTotal)

		if hasAppeal > 0 {
			c.ResolveRecordAppeal()
		}
	}
	return nil
}

var _ automod.IdentityRuleFunc = ResolveAppealOnAccountDeleteRule

func ResolveAppealOnAccountDeleteRule(c *automod.AccountContext) error {
	hasAppeal := c.GetCount("appeal", c.Account.Identity.DID.String(), countstore.PeriodTotal)

	// @TODO: Check here that we check if the account has been deleted or not before resolving
	// This is not currently available on the context
	if hasAppeal > 0 && (c.Account.Deactivated) {
		c.ResolveAccountAppeal()
	}
	return nil
}

var _ automod.OzoneEventRuleFunc = MarkAppealOzoneEventRule

// looks for appeals on records/accounts and flags subjects
func MarkAppealOzoneEventRule(c *automod.OzoneEventContext) error {
	isResolveAppealEvent := c.Event.Event.ModerationDefs_ModEventResolveAppeal != nil
	// appeals are just report events emitted by the author of the reported content with a special report type
	isAppealEvent := c.Event.Event.ModerationDefs_ModEventReport != nil && *c.Event.Event.ModerationDefs_ModEventReport.ReportType == "com.atproto.moderation.defs#reasonAppeal"

	if !isAppealEvent && !isResolveAppealEvent {
		return nil
	}

	counterKey := c.Event.SubjectDID.String()
	if c.Event.SubjectURI != nil {
		counterKey = c.Event.SubjectURI.String()
	}

	if isAppealEvent {
		c.Increment("appealed", counterKey)
	} else {
		// @TODO: We should reset the appeal counter here but there doesn't seem to be an api for it
		// c.Reset("appealed", counterKey)
	}

	return nil
}
