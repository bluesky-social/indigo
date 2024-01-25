package rules

import (
	"net/url"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/automod/countstore"
)

var _ automod.IdentityRuleFunc = NewAccountRule

// triggers on first identity event for an account (DID)
func NewAccountRule(c *automod.AccountContext) error {
	// need access to IndexedAt for this rule
	if c.Account.Private == nil || c.Account.Identity == nil {
		return nil
	}

	did := c.Account.Identity.DID.String()
	age := time.Since(c.Account.Private.IndexedAt)
	if age > 2*time.Hour {
		return nil
	}
	exists := c.GetCount("acct/exists", did, countstore.PeriodTotal)
	if exists == 0 {
		c.Logger.Info("new account")
		c.Increment("acct/exists", did)

		pdsURL, err := url.Parse(c.Account.Identity.PDSEndpoint())
		if err != nil {
			c.Logger.Warn("invalid PDS URL", "err", err, "endpoint", c.Account.Identity.PDSEndpoint())
			return nil
		}
		pdsHost := strings.ToLower(pdsURL.Host)
		existingAccounts := c.GetCount("host/newacct", pdsHost, countstore.PeriodTotal)
		c.Increment("host/newacct", pdsHost)

		// new PDS host
		if existingAccounts == 0 {
			c.Logger.Info("new PDS instance", "host", pdsHost)
			c.Increment("host", "new")
			c.AddAccountFlag("host-first-account")
			c.Notify("slack")
		}
	}
	return nil
}
