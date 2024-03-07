package rules

import (
	"fmt"
	"strings"
	"time"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/automod"
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
			}
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
		}
	}
	return nil
}

var _ automod.IdentityRuleFunc = NewAccountBotEmailRule

func NewAccountBotEmailRule(c *automod.AccountContext) error {
	// need access to IndexedAt for this rule
	if c.Account.Private == nil || c.Account.Identity == nil {
		return nil
	}

	age := time.Since(c.Account.Private.IndexedAt)
	if age > 1*time.Hour {
		return nil
	}

	for _, tld := range botSpamTLDs {
		if strings.HasSuffix(c.Account.Private.Email, tld) {
			c.AddAccountFlag("new-suspicious-email")
			c.ReportAccount(automod.ReportReasonSpam, fmt.Sprintf("possible bot based on email domain TLD: %s", tld))
			c.Notify("slack")
		}
	}
	return nil
}
