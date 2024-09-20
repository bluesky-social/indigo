package rules

import (
	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/automod/countstore"
)

var _ automod.RecordRuleFunc = OzoneRecordHistoryPersistRule

func OzoneRecordHistoryPersistRule(c *automod.RecordContext) error {
	switch c.RecordOp.Collection {
	case "app.bsky.labeler.service":
		c.PersistRecordOzoneEvent()
	case "app.bsky.feed.post":
		// @TODO: we should probably persist if a deleted post has reports on it but right now, we are not keeping track of reports on records
		if c.RecordOp.Action == "delete" {
			// If a post being deleted has an active appeal, persist the event
			if c.GetCount("appeal", c.RecordOp.ATURI().String(), countstore.PeriodTotal) > 0 {
				c.PersistRecordOzoneEvent()
			}

		}
	case "app.bsky.actor.profile":
		hasLabels := len(c.Account.AccountLabels) > 0
		// If there is an appeal on the account or the profile record
		// Appeal counts are reset when appeals are resolved so this should only be true when there is an unresolved appeal
		hasAppeals := c.GetCount("appeal", c.Account.Identity.DID.String(), countstore.PeriodTotal) > 0 || c.GetCount("appeal", c.RecordOp.ATURI().String(), countstore.PeriodTotal) > 0
		if hasLabels || hasAppeals {
			c.PersistRecordOzoneEvent()
		}
	}
	return nil
}
