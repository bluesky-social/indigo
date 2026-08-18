package rules

import (
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/automod/keyword"
)

var _ automod.PostRuleFunc = AccountDemoPostRule

// this is a dummy rule to demonstrate accessing account metadata (eg, profile) from within post handler
func AccountDemoPostRule(c *automod.RecordContext, post *appbsky.FeedPost) error {
	if c.Account.Profile.Description != nil && len(post.Text) > 5 && *c.Account.Profile.Description == post.Text {
		c.AddRecordFlag("own-profile-description")
		c.Notify("slack")
	}
	return nil
}

func CelebSpamProfileRule(c *automod.RecordContext, profile *appbsky.ActorProfile) error {
	anyElon := false
	anyMusk := false
	if profile.DisplayName != nil {
		tokens := keyword.TokenizeText(*profile.DisplayName)
		for _, tok := range tokens {
			if tok == "elon" {
				anyElon = true
			}
			if tok == "musk" {
				anyMusk = true
			}
		}
	}
	if anyElon && anyMusk {
		c.AddRecordFlag("profile-elon-musk")
		c.ReportAccount(automod.ReportReasonSpam, "possible Elon Musk impersonator")
		return nil
	}
	return nil
}

var _ automod.ProfileRuleFunc = CelebSpamProfileRule
