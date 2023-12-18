package rules

import (
	"context"
	"strings"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/automod"
)

var _ automod.PostRuleFunc = AccountPrivateDemoPostRule

// dummy rule. this leaks PII (account email) in logs and should never be used in real life
func AccountPrivateDemoPostRule(ctx context.Context, evt *automod.RecordEvent, post *appbsky.FeedPost) error {
	if evt.Account.Private != nil {
		if strings.HasSuffix(evt.Account.Private.Email, "@blueskyweb.xyz") {
			evt.Logger.Info("hello dev!", "email", evt.Account.Private.Email)
		}
	}
	return nil
}
