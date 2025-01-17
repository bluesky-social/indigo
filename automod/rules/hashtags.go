package rules

import (
	"fmt"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/automod/helpers"
	"github.com/bluesky-social/indigo/automod/keyword"
)

// looks for specific hashtags from known lists
func BadHashtagsPostRule(c *automod.RecordContext, post *appbsky.FeedPost) error {
	for _, tag := range helpers.ExtractHashtagsPost(post) {
		tag = helpers.NormalizeHashtag(tag)
		// skip some bad-word hashtags which frequently false-positive
		if tag == "nazi" || tag == "hitler" {
			continue
		}
		if c.InSet("bad-hashtags", tag) || c.InSet("bad-words", tag) {
			c.AddRecordFlag("bad-hashtag")
			c.ReportRecord(automod.ReportReasonRude, fmt.Sprintf("possible bad word in hashtags: %s", tag))
			break
		}
		word := keyword.SlugContainsExplicitSlur(keyword.Slugify(tag))
		if word != "" {
			c.AddAccountFlag("bad-hashtag")
			c.ReportRecord(automod.ReportReasonRude, fmt.Sprintf("possible bad word in hashtags: %s", word))
			break
		}
	}
	return nil
}

var _ automod.PostRuleFunc = BadHashtagsPostRule

// if a post is "almost all" hashtags, it might be a form of search spam
func TooManyHashtagsPostRule(c *automod.RecordContext, post *appbsky.FeedPost) error {
	tags := helpers.ExtractHashtagsPost(post)
	tagChars := 0
	for _, tag := range tags {
		tagChars += len(tag)
	}
	tagTextRatio := float64(tagChars) / float64(len(post.Text))
	// if there is an image, allow some more tags
	if len(tags) > 4 && tagTextRatio > 0.6 && post.Embed.EmbedImages == nil {
		c.AddRecordFlag("many-hashtags")
		c.Notify("slack")
	} else if len(tags) > 7 && tagTextRatio > 0.8 {
		c.AddRecordFlag("many-hashtags")
		c.Notify("slack")
	}
	return nil
}

var _ automod.PostRuleFunc = TooManyHashtagsPostRule
