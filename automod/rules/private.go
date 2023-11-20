package rules

import (
	"strings"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/automod"
)

// dummy rule. this leaks PII (account email) in logs and should never be used in real life
func AccountPrivateDemoPostRule(evt *automod.RecordEvent, post *appbsky.FeedPost) error {
	if evt.Account.Private != nil {
		if strings.HasSuffix(evt.Account.Private.Email, "@blueskyweb.xyz") {
			evt.Logger.Info("hello dev!", "email", evt.Account.Private.Email)
		}
	}
	return nil
}
