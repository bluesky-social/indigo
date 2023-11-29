package rules

import (
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/automod"
)

// looks for specific hashtags from known lists
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

// if a post is "almost all" hashtags, it might be a form of search spam
func TooManyHashtagsPostRule(evt *automod.RecordEvent, post *appbsky.FeedPost) error {
	tags := ExtractHashtags(post)
	tagChars := 0
	for _, tag := range tags {
		tagChars += len(tag)
	}
	if len(tags) >= 4 && (float64(tagChars)/float64(len(post.Text)) > 0.6) {
		evt.AddRecordFlag("many-hashtags")
		// TODO: consider reporting or labeling for spam
	}
	return nil
}
