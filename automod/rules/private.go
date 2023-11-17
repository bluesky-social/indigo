package rules

import (
	"strings"

	"github.com/bluesky-social/indigo/automod"
)

// dummy rule. this leaks PII (account email) in logs and should never be used in real life
func AccountPrivateDemoPostRule(evt *automod.PostEvent) error {
	if evt.Account.Private != nil {
		if strings.HasSuffix(evt.Account.Private.Email, "@blueskyweb.xyz") {
			evt.Logger.Info("hello dev!", "email", evt.Account.Private.Email)
		}
	}
	return nil
}
