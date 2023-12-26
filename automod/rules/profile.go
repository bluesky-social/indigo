package rules

import (
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/automod"
)

// this is a dummy rule to demonstrate accessing account metadata (eg, profile) from within post handler
func AccountDemoPostRule(c *automod.RecordContext, post *appbsky.FeedPost) error {
	if c.Account.Profile.Description != nil && len(post.Text) > 5 && *c.Account.Profile.Description == post.Text {
		c.AddRecordFlag("own-profile-description")
	}
	return nil
}
