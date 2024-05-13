package rules

import (
	"fmt"
	"strings"
	"time"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/automod"
)

var _ automod.PostRuleFunc = NostrSpamPostRule

// looks for new accounts, which frequently post the same type of content
func NostrSpamPostRule(c *automod.RecordContext, post *appbsky.FeedPost) error {
	if c.Account.Identity == nil {
		return nil
	}

	// often don't have private metadata for these accounts right after creation
	if c.Account.Private != nil {
		// TODO: helper for account age; and use public info for this (not private)
		age := time.Since(c.Account.Private.IndexedAt)
		if age > 2*24*time.Hour {
			return nil
		}
	}

	// is this a bridged nostr account? if not, bail out
	hdl := c.Account.Identity.Handle.String()
	if !(strings.HasPrefix(hdl, "npub") && len(hdl) > 63 && strings.HasSuffix(hdl, ".brid.gy")) {
		return nil
	}

	c.AddAccountFlag("nostr")

	// only posts with dumb patterns (for now)
	txt := strings.ToLower(post.Text)
	if !c.InSet("trivial-spam-text", txt) {
		return nil
	}

	// only accounts with empty profile (for now)
	if c.Account.Profile.HasAvatar || c.Account.Profile.Description != nil {
		return nil
	}

	c.ReportAccount(automod.ReportReasonOther, fmt.Sprintf("likely nostr spam account (also labeled; remove label if this isn't spam!)"))
	c.AddAccountLabel("!hide")
	c.Notify("slack")
	return nil
}
