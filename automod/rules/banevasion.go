package rules

import (
	"fmt"
	"strings"

	"github.com/bluesky-social/indigo/automod"
)

// example rule showing how to detect a pattern in handles
func BanEvasionHandleRule(c *automod.AccountContext) error {

	// TODO: move these to config
	patterns := []string{
		"rapeskids",
		"isapedo",
		"thegroomer",
		"isarapist",
	}

	handle := c.Account.Identity.Handle.Normalize().String()
	email := ""
	if c.Account.Private != nil {
		email = strings.ToLower(c.Account.Private.Email)
	}
	for _, w := range patterns {
		if strings.Contains(handle, w) {
			c.AddAccountFlag("evasion-pattern-handle")
			c.ReportAccount(automod.ReportReasonViolation, fmt.Sprintf("possible ban evasion pattern (username): %s", w))
			c.Notify("slack")
			break
		}
		if email != "" && strings.Contains(email, w) {
			c.AddAccountFlag("evasion-pattern-email")
			c.ReportAccount(automod.ReportReasonViolation, fmt.Sprintf("possible ban evasion pattern (email): %s", w))
			c.Notify("slack")
			break
		}
	}
	return nil
}

var _ automod.IdentityRuleFunc = BanEvasionHandleRule
