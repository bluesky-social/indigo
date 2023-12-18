package rules

import (
	"context"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/automod"
)

var _ automod.PostRuleFunc = AccountDemoPostRule

// this is a dummy rule to demonstrate accessing account metadata (eg, profile) from within post handler
func AccountDemoPostRule(ctx context.Context, evt *automod.RecordEvent, post *appbsky.FeedPost) error {
	if evt.Account.Profile.Description != nil && len(post.Text) > 5 && *evt.Account.Profile.Description == post.Text {
		evt.AddRecordFlag("own-profile-description")
	}
	return nil
}
