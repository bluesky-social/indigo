package rules

import (
	"fmt"
	"strings"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/automod/keyword"
)

var _ automod.PostRuleFunc = BadWordPostRule

func BadWordPostRule(c *automod.RecordContext, post *appbsky.FeedPost) error {
	for _, tok := range ExtractTextTokensPost(post) {
		tok = strings.TrimSuffix(tok, "s")
		if c.InSet("worst-words", tok) {
			c.AddRecordFlag("bad-word")
			break
		}
	}
	word := keyword.SlugContainsExplicitSlur(keyword.Slugify(post.Text))
	if word != "" {
		c.AddRecordFlag("bad-word")
	}
	return nil
}

var _ automod.ProfileRuleFunc = BadWordProfileRule

func BadWordProfileRule(c *automod.RecordContext, profile *appbsky.ActorProfile) error {
	for _, tok := range ExtractTextTokensProfile(profile) {
		// de-pluralize
		tok = strings.TrimSuffix(tok, "s")
		if c.InSet("worst-words", tok) {
			c.AddRecordFlag("bad-word")
			break
		}
	}
	if profile.Description != nil {
		word := keyword.SlugContainsExplicitSlur(keyword.Slugify(*profile.Description))
		if word != "" {
			c.AddRecordFlag("bad-word")
		}
	}
	return nil
}

var _ automod.PostRuleFunc = ReplySingleBadWordPostRule

func ReplySingleBadWordPostRule(c *automod.RecordContext, post *appbsky.FeedPost) error {
	if post.Reply != nil && !IsSelfThread(c, post) {
		tokens := ExtractTextTokensPost(post)
		if len(tokens) != 1 {
			return nil
		}
		tok := tokens[0]
		if c.InSet("worst-words", tok) || c.InSet("bad-words", tok)  {
			c.AddRecordFlag("reply-single-bad-word")
			c.ReportRecord(automod.ReportReasonRude, fmt.Sprintf("single-bad-word reply: %s", tok))
		}
	}
	return nil
}

var _ automod.RecordRuleFunc = RecordKeyBadWordRecordRule

func RecordKeyBadWordRecordRule(c *automod.RecordContext) error {
	word := keyword.SlugContainsExplicitSlur(keyword.Slugify(c.RecordOp.RecordKey.String()))
	if word != "" {
		c.AddRecordFlag("bad-word-record-key")
	}

	tokens := keyword.TokenizeIdentifier(c.RecordOp.RecordKey.String())
	for _, tok := range tokens {
		if c.InSet("worst-words", tok) {
			c.AddRecordFlag("bad-word-record-key")
		}
	}

	return nil
}

var _ automod.IdentityRuleFunc = HandleBadWordIdentityRule

func HandleBadWordIdentityRule(c *automod.AccountContext) error {
	word := keyword.SlugContainsExplicitSlur(keyword.Slugify(c.Account.Identity.Handle.String()))
	if word != "" {
		c.AddAccountFlag("bad-word-handle")
	}

	tokens := keyword.TokenizeIdentifier(c.Account.Identity.Handle.String())
	for _, tok := range tokens {
		if c.InSet("worst-words", tok) {
			c.AddAccountFlag("bad-word-handle")
		}
	}

	return nil
}
