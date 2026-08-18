package rules

import (
	"fmt"
	"strings"
	"time"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/automod/helpers"
)

var botLinkStrings = []string{"ainna13762491", "LINK押して", "→ https://tiny", "⇒ http://tiny"}
var botSpamTLDs = []string{".today", ".life"}
var botSpamStrings = []string{"515-9719"}

var _ automod.ProfileRuleFunc = BotLinkProfileRule

func BotLinkProfileRule(c *automod.RecordContext, profile *appbsky.ActorProfile) error {
	if profile.Description != nil {
		for _, str := range botLinkStrings {
			if strings.Contains(*profile.Description, str) {
				c.AddAccountFlag("profile-bot-string")
				c.AddAccountLabel("spam")
				c.ReportAccount(automod.ReportReasonSpam, fmt.Sprintf("possible bot based on link in profile: %s", str))
				c.Notify("slack")
				return nil
			}
		}
		if strings.Contains(*profile.Description, "🏈🍕🌀") {
			c.AddAccountFlag("profile-bot-string")
			c.ReportAccount(automod.ReportReasonSpam, "possible bot based on string in profile")
			c.Notify("slack")
			return nil
		}
	}
	return nil
}

var _ automod.PostRuleFunc = SimpleBotPostRule

func SimpleBotPostRule(c *automod.RecordContext, post *appbsky.FeedPost) error {
	for _, str := range botSpamStrings {
		if strings.Contains(post.Text, str) {
			// NOTE: reporting the *account* not individual posts
			c.AddAccountFlag("post-bot-string")
			c.ReportAccount(automod.ReportReasonSpam, fmt.Sprintf("possible bot based on string in post: %s", str))
			c.Notify("slack")
			return nil
		}
	}
	return nil
}

var _ automod.IdentityRuleFunc = NewAccountBotEmailRule

func NewAccountBotEmailRule(c *automod.AccountContext) error {
	if c.Account.Identity == nil || !helpers.AccountIsYoungerThan(c, 1*time.Hour) {
		return nil
	}

	for _, tld := range botSpamTLDs {
		if strings.HasSuffix(c.Account.Private.Email, tld) {
			c.AddAccountFlag("new-suspicious-email")
			c.ReportAccount(automod.ReportReasonSpam, fmt.Sprintf("possible bot based on email domain TLD: %s", tld))
			c.Notify("slack")
			return nil
		}
	}
	return nil
}

var _ automod.PostRuleFunc = TrivialSpamPostRule

// looks for new accounts, which frequently post the same type of content
func TrivialSpamPostRule(c *automod.RecordContext, post *appbsky.FeedPost) error {
	if c.Account.Identity == nil || !helpers.AccountIsYoungerThan(&c.AccountContext, 8*24*time.Hour) {
		return nil
	}

	// only posts with dumb patterns (for now)
	txt := strings.ToLower(post.Text)
	if !c.InSet("trivial-spam-text", txt) {
		return nil
	}

	// only accounts with empty profile (for now)
	if c.Account.Profile.HasAvatar {
		return nil
	}

	c.ReportAccount(automod.ReportReasonOther, "trivial spam account (also labeled; remove label if this isn't spam!)")
	c.AddAccountLabel("!hide")
	c.Notify("slack")
	return nil
}
