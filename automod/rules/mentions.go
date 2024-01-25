package rules

import (
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/automod/countstore"
)

var _ automod.PostRuleFunc = DistinctMentionsRule

var mentionHourlyThreshold = 40

// DistinctMentionsRule looks for accounts which mention an unusually large number of distinct accounts per period.
func DistinctMentionsRule(c *automod.RecordContext, post *appbsky.FeedPost) error {
	did := c.Account.Identity.DID.String()

	// Increment counters for all new mentions in this post.
	var newMentions bool
	for _, facet := range post.Facets {
		for _, feature := range facet.Features {
			mention := feature.RichtextFacet_Mention
			if mention == nil {
				continue
			}
			c.IncrementDistinct("mentions", did, mention.Did)
			newMentions = true
		}
	}

	// If there were any new mentions, check if it's gotten spammy.
	if !newMentions {
		return nil
	}
	if mentionHourlyThreshold <= c.GetCountDistinct("mentions", did, countstore.PeriodHour) {
		c.AddAccountFlag("high-distinct-mentions")
		c.Notify("slack")
	}

	return nil
}
