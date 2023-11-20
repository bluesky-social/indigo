package rules

import (
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/automod"
)

// this is a dummy rule to demonstrate accessing account metadata (eg, profile) from within post handler
func AccountDemoPostRule(evt *automod.RecordEvent, post *appbsky.FeedPost) error {
	if evt.Account.Profile.Description != nil && len(post.Text) > 5 && *evt.Account.Profile.Description == post.Text {
		evt.AddRecordFlag("own-profile-description")
	}
	return nil
}
