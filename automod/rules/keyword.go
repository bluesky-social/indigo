package rules

import (
	"fmt"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/automod"
)

func KeywordPostRule(c *automod.RecordContext, post *appbsky.FeedPost) error {
	for _, tok := range ExtractTextTokensPost(post) {
		if c.InSet("bad-words", tok) {
			c.AddRecordFlag("bad-word")
			c.ReportRecord(automod.ReportReasonRude, fmt.Sprintf("bad-word: %s", tok))
			break
		}
	}
	return nil
}

func KeywordProfileRule(c *automod.RecordContext, profile *appbsky.ActorProfile) error {
	for _, tok := range ExtractTextTokensProfile(profile) {
		if c.InSet("bad-words", tok) {
			c.AddRecordFlag("bad-word")
			c.ReportRecord(automod.ReportReasonRude, fmt.Sprintf("bad-word: %s", tok))
			break
		}
	}
	return nil
}

func ReplySingleKeywordPostRule(c *automod.RecordContext, post *appbsky.FeedPost) error {
	if post.Reply != nil && !IsSelfThread(c, post) {
		tokens := ExtractTextTokensPost(post)
		if len(tokens) == 1 && c.InSet("bad-words", tokens[0]) {
			c.AddRecordFlag("reply-single-bad-word")
		}
	}
	return nil
}
