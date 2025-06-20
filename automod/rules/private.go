package rules

import (
	"strings"

	appgndr "github.com/gander-social/gander-indigo-sovereign/api/gndr"
	"github.com/gander-social/gander-indigo-sovereign/automod"
)

var _ automod.PostRuleFunc = AccountPrivateDemoPostRule

// dummy rule. this leaks PII (account email) in logs and should never be used in real life
func AccountPrivateDemoPostRule(c *automod.RecordContext, post *appgndr.FeedPost) error {
	if c.Account.Private != nil {
		if strings.HasSuffix(c.Account.Private.Email, "@ganderweb.xyz") {
			c.Logger.Info("hello dev!", "email", c.Account.Private.Email)
		}
	}
	return nil
}
