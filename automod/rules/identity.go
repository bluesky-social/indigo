package rules

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/automod/countstore"
)

var _ automod.IdentityRuleFunc = NewAccountRule

// triggers on first identity event for an account (DID)
func NewAccountRule(ctx context.Context, evt *automod.IdentityEvent) error {
	// need access to IndexedAt for this rule
	if evt.Account.Private == nil || evt.Account.Identity == nil {
		return nil
	}

	did := evt.Account.Identity.DID.String()
	age := time.Since(evt.Account.Private.IndexedAt)
	if age > 2*time.Hour {
		return nil
	}
	exists := evt.GetCount("acct/exists", did, countstore.PeriodTotal)
	if exists == 0 {
		evt.Logger.Info("new account")
		evt.Increment("acct/exists", did)

		pdsURL, err := url.Parse(evt.Account.Identity.PDSEndpoint())
		if err != nil {
			evt.Logger.Warn("invalid PDS URL", "err", err, "endpoint", evt.Account.Identity.PDSEndpoint())
			return nil
		}
		pdsHost := strings.ToLower(pdsURL.Host)
		existingAccounts := evt.GetCount("host/newacct", pdsHost, countstore.PeriodTotal)
		evt.Increment("host/newacct", pdsHost)

		// new PDS host
		if existingAccounts == 0 {
			evt.Logger.Info("new PDS instance", "host", pdsHost)
			evt.Increment("host", "new")
			evt.AddAccountFlag("host-first-account")
		}
	}
	return nil
}
