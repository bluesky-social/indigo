package rules

import (
	"fmt"
	"time"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/automod/countstore"
	"github.com/bluesky-social/indigo/automod/helpers"
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

var youngMentionAccountLimit = 12
var _ automod.PostRuleFunc = YoungAccountDistinctMentionsRule

func YoungAccountDistinctMentionsRule(c *automod.RecordContext, post *appbsky.FeedPost) error {
	if c.Account.Identity == nil || !helpers.AccountIsYoungerThan(&c.AccountContext, 14*24*time.Hour) {
		return nil
	}

	// parse out all the mentions
	var mentionedAccounts []syntax.DID
	for _, facet := range post.Facets {
		for _, feature := range facet.Features {
			mention := feature.RichtextFacet_Mention
			if mention == nil {
				continue
			}
			did, err := syntax.ParseDID(mention.Did)
			if err != nil {
				continue
			}
			mentionedAccounts = append(mentionedAccounts, did)
		}
	}
	if len(mentionedAccounts) == 0 {
		return nil
	}

	did := c.Account.Identity.DID.String()

	// check for relationships, and increment accounts
	newMentions := 0
	for _, otherDID := range mentionedAccounts {
		rel := c.GetAccountRelationship(otherDID)
		if rel.FollowedBy {
			continue
		}
		c.IncrementDistinct("young-mention", did, otherDID.String())
		newMentions += 1
	}

	count := c.GetCountDistinct("young-mention", did, countstore.PeriodHour) + newMentions
	if count >= youngMentionAccountLimit {
		c.AddAccountFlag("new-account-distinct-account-mention")
		c.ReportAccount(automod.ReportReasonRude, fmt.Sprintf("possible spam (new account, mentioned %d distinct accounts in past hour)", count))
		c.Notify("slack")
	}

	return nil
}
