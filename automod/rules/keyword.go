package rules

import (
	"fmt"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/automod"
)

func KeywordPostRule(evt *automod.RecordEvent, post *appbsky.FeedPost) error {
	for _, tok := range ExtractTextTokensPost(post) {
		if evt.InSet("bad-words", tok) {
			evt.AddRecordFlag("bad-word")
			evt.ReportRecord(automod.ReportReasonRude, fmt.Sprintf("bad-word: %s", tok))
			break
		}
	}
	return nil
}

func KeywordProfileRule(evt *automod.RecordEvent, profile *appbsky.ActorProfile) error {
	for _, tok := range ExtractTextTokensProfile(profile) {
		if evt.InSet("bad-words", tok) {
			evt.AddRecordFlag("bad-word")
			evt.ReportRecord(automod.ReportReasonRude, fmt.Sprintf("bad-word: %s", tok))
			break
		}
	}
	return nil
}

func ReplySingleKeywordPostRule(evt *automod.RecordEvent, post *appbsky.FeedPost) error {
	if post.Reply != nil && !IsSelfThread(evt, post) {
		tokens := ExtractTextTokensPost(post)
		if len(tokens) == 1 && evt.InSet("bad-words", tokens[0]) {
			evt.AddRecordFlag("reply-single-bad-word")
		}
	}
	return nil
}
