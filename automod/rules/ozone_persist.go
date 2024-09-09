package rules

import (
	"github.com/bluesky-social/indigo/automod"
)

var _ automod.RecordRuleFunc = OzoneRecordHistoryPersistRule

func OzoneRecordHistoryPersistRule(c *automod.RecordContext) error {
	// TODO: flesh out this logic
	// based on record type
	switch c.RecordOp.Collection {
	case "app.bsky.labeler.service":
		c.PersistRecordOzoneEvent()
	case "app.bsky.feed.post":
		if c.RecordOp.Action == "update" {
		}
	case "app.bsky.actor.profile":
		// TODO: fix this logic
		if len(c.Account.AccountLabels) > 2 {
			c.PersistRecordOzoneEvent()
		}
	}
	return nil
}
