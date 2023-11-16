package rules

import (
	"github.com/bluesky-social/indigo/automod"
)

// this is a dummy rule to demonstrate accessing account metadata (eg, profile) from within post handler
func AccountDemoPostRule(evt *automod.PostEvent) error {
	if evt.Account.Profile.Description != nil && len(evt.Post.Text) > 5 && *evt.Account.Profile.Description == evt.Post.Text {
		evt.AddRecordFlag("own-profile-description")
	}
	return nil
}
