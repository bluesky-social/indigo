package rules

import (
	"strings"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/automod"
)

var _ automod.PostRuleFunc = AccountPrivateDemoPostRule

// dummy rule. this leaks PII (account email) in logs and should never be used in real life
func AccountPrivateDemoPostRule(c *automod.RecordContext, post *appbsky.FeedPost) error {
	if c.Account.Private != nil {
		if strings.HasSuffix(c.Account.Private.Email, "@blueskyweb.xyz") {
			c.Logger.Info("hello dev!", "email", c.Account.Private.Email)
		}
	}
	return nil
}
