package rules

import (
	"github.com/bluesky-social/indigo/automod"
)

var _ automod.RecordRuleFunc = OzoneRecordHistoryPersistRule

func OzoneRecordHistoryPersistRule(c *automod.RecordContext) error {
	switch c.RecordOp.Collection {
	case "app.bsky.labeler.service":
		c.PersistRecordOzoneEvent()
	case "app.bsky.feed.post":
		if c.RecordOp.Action == "delete" {
			if c.GetCount("mod-event", c.RecordOp.ATURI().String(), automod.PeriodTotal) > 0 {
				c.PersistRecordOzoneEvent()
			}
		}
	case "app.bsky.actor.profile":
		if c.GetCount("mod-event", c.RecordOp.ATURI().String(), automod.PeriodTotal) > 0 {
			c.PersistRecordOzoneEvent()
		}
	}
	return nil
}
