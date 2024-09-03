package rules

import (
	"github.com/bluesky-social/indigo/automod"
)

var _ automod.RecordRuleFunc = ResolveAppealOnDeleteRule

func ResolveAppealOnDeleteRule(c *automod.RecordContext) error {
	switch c.RecordOp.Collection {
	case "app.bsky.feed.post":
		c.ResolveRecordAppeal()
	}
	return nil
}
