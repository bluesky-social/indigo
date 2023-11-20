package rules

import (
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/automod"
)

func BanHashtagsPostRule(evt *automod.RecordEvent, post *appbsky.FeedPost) error {
	for _, tag := range ExtractHashtags(post) {
		if evt.InSet("banned-hashtags", tag) {
			evt.AddRecordLabel("bad-hashtag")
			break
		}
	}
	return nil
}
