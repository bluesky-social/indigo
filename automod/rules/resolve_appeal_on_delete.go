package rules

import (
	"github.com/bluesky-social/indigo/automod"
)

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
		c.Increment("appeal", counterKey)
	} else {
		c.ResetCounter("appeal", counterKey)
	}

	return nil
}
