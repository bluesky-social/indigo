package rules

import (
	"context"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/automod"
)

var (
	_ automod.PostRuleFunc    = KeywordPostRule
	_ automod.ProfileRuleFunc = KeywordProfileRule
	_ automod.PostRuleFunc    = ReplySingleKeywordPostRule
)

func KeywordPostRule(ctx context.Context, evt *automod.RecordEvent, post *appbsky.FeedPost) error {
	for _, tok := range ExtractTextTokensPost(post) {
		if evt.InSet("bad-words", tok) {
			evt.AddRecordFlag("bad-word")
			break
		}
	}
	return nil
}

func KeywordProfileRule(ctx context.Context, evt *automod.RecordEvent, profile *appbsky.ActorProfile) error {
	for _, tok := range ExtractTextTokensProfile(profile) {
		if evt.InSet("bad-words", tok) {
			evt.AddRecordFlag("bad-word")
			break
		}
	}
	return nil
}

func ReplySingleKeywordPostRule(ctx context.Context, evt *automod.RecordEvent, post *appbsky.FeedPost) error {
	if post.Reply != nil && !IsSelfThread(evt, post) {
		tokens := ExtractTextTokensPost(post)
		if len(tokens) == 1 && evt.InSet("bad-words", tokens[0]) {
			evt.AddRecordFlag("reply-single-bad-word")
		}
	}
	return nil
}
