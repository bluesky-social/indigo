package rules

import (
	"fmt"
	"strings"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/automod"
)

var botLinkStrings = []string{"ainna13762491"}

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
