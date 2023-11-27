package rules

import (
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/automod"
)

func BadHashtagsPostRule(evt *automod.RecordEvent, post *appbsky.FeedPost) error {
	for _, tag := range ExtractHashtags(post) {
		tag = NormalizeHashtag(tag)
		if evt.InSet("bad-hashtags", tag) {
			evt.AddRecordFlag("bad-hashtag")
			break
		}
	}
	return nil
}
