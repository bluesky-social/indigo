package rules

import (
	"net/url"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/automod/countstore"
	"github.com/bluesky-social/indigo/automod/helpers"
)

// triggers on first identity event for an account (DID)
func NewAccountRule(c *automod.AccountContext) error {
	if c.Account.Identity == nil || !helpers.AccountIsYoungerThan(c, 4*time.Hour) {
		return nil
	}

	did := c.Account.Identity.DID.String()
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
			c.Logger.Info("new PDS instance", "pdsHost", pdsHost)
			c.Increment("host", "new")
			c.AddAccountFlag("host-first-account")
			c.Notify("slack")
		}
	}
	return nil
}

var _ automod.IdentityRuleFunc = NewAccountRule

func CelebSpamIdentityRule(c *automod.AccountContext) error {

	hdl := c.Account.Identity.Handle.String()
	if strings.Contains(hdl, "elon") && strings.Contains(hdl, "musk") {
		c.AddAccountFlag("handle-elon-musk")
		c.ReportAccount(automod.ReportReasonSpam, "possible Elon Musk impersonator")
		return nil
	}

	return nil
}

var _ automod.IdentityRuleFunc = CelebSpamIdentityRule
