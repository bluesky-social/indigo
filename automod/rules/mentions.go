package rules

import (
	"context"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/automod"
)

var _ automod.PostRuleFunc = DistinctMentionsRule

var mentionHourlyThreshold = 20

// DistinctMentionsRule looks for accounts which mention an unusually large number of distinct accounts per period.
func DistinctMentionsRule(ctx context.Context, evt *automod.RecordEvent, post *appbsky.FeedPost) error {
	did := evt.Account.Identity.DID.String()

	// Increment counters for all new mentions in this post.
	var newMentions bool
	for _, facet := range post.Facets {
		for _, feature := range facet.Features {
			mention := feature.RichtextFacet_Mention
			if mention == nil {
				continue
			}
			evt.IncrementDistinct("mentions", did, mention.Did)
			newMentions = true
		}
	}

	// If there were any new mentions, check if it's gotten spammy.
	if !newMentions {
		return nil
	}
	if mentionHourlyThreshold <= evt.GetCountDistinct("mentions", did, automod.PeriodHour) {
		evt.AddAccountFlag("high-distinct-mentions")
	}

	return nil
}
