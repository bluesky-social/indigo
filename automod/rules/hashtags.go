package rules

import (
	"github.com/bluesky-social/indigo/automod"
)

func BanHashtagsPostRule(evt *automod.PostEvent) error {
	for _, tag := range ExtractHashtags(evt.Post) {
		if evt.InSet("banned-hashtags", tag) {
			evt.AddRecordLabel("bad-hashtag")
			break
		}
	}
	return nil
}
