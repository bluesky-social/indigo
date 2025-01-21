package rules

import (
	"github.com/bluesky-social/indigo/automod"
)

var _ automod.OzoneEventRuleFunc = CountModEventRule

// looks for appeals on records/accounts and flags subjects
func CountModEventRule(c *automod.OzoneEventContext) error {
	counterKey := c.Event.SubjectDID.String()
	if c.Event.SubjectURI != nil {
		counterKey = c.Event.SubjectURI.String()
	}

	c.Increment("ozone-event", counterKey)
	c.Logger.Debug("ozone event count", "subject", counterKey, "count", c.GetCount("ozone-event", counterKey, automod.PeriodTotal))

	return nil
}
