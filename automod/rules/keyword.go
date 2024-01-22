package rules

import (
	"fmt"
	"strings"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/automod/keyword"
)

func BadWordPostRule(c *automod.RecordContext, post *appbsky.FeedPost) error {
	word := keyword.SlugContainsExplicitSlur(keyword.Slugify(post.Text))
	if word != "" {
		c.AddRecordFlag("explicit-slur-text")
	}

	for _, tok := range ExtractTextTokensPost(post) {
		// de-pluralize
		tok = strings.TrimSuffix(tok, "s")
		if c.InSet("worst-words", tok) {
			c.AddRecordFlag("bad-word")
			break
		}
	}
	return nil
}

var _ automod.PostRuleFunc = BadWordPostRule

func BadWordProfileRule(c *automod.RecordContext, profile *appbsky.ActorProfile) error {
	if profile.Description != nil {
		word := keyword.SlugContainsExplicitSlur(keyword.Slugify(*profile.Description))
		if word != "" {
			c.AddRecordFlag("explicit-slur-description")
		}
	}
	if profile.DisplayName != nil {
		word := keyword.SlugContainsExplicitSlur(keyword.Slugify(*profile.DisplayName))
		if word != "" {
			c.AddRecordFlag("explicit-slur-name")
		}
	}
	for _, tok := range ExtractTextTokensProfile(profile) {
		// de-pluralize
		tok = strings.TrimSuffix(tok, "s")
		if c.InSet("bad-words", tok) {
			c.AddRecordFlag("bad-word")
			break
		}
	}
	return nil
}

var _ automod.ProfileRuleFunc = BadWordProfileRule

// looks for the specific harassment situation of a replay to another user with only a single word
func ReplySingleBadWordPostRule(c *automod.RecordContext, post *appbsky.FeedPost) error {
	if post.Reply != nil && !IsSelfThread(c, post) {
		tokens := ExtractTextTokensPost(post)
		if len(tokens) != 1 {
			return nil
		}
		tok := tokens[0]
		if c.InSet("worst-words", tok) || c.InSet("bad-words", tok) {
			c.AddRecordFlag("reply-single-bad-word")
			c.ReportRecord(automod.ReportReasonRude, fmt.Sprintf("single-bad-word reply: %s", tok))
		}
	}
	return nil
}

var _ automod.PostRuleFunc = ReplySingleBadWordPostRule

// scans for bad keywords in records other than posts and profiles
func BadWordOtherRecordRule(c *automod.RecordContext) error {
	name := ""
	text := ""
	switch c.RecordOp.Collection.String() {
	case "app.bsky.graph.list":
		list, ok := c.RecordOp.Value.(*appbsky.GraphList)
		if !ok {
			return fmt.Errorf("mismatch between collection (%s) and type", c.RecordOp.Collection)
		}
		name += " " + list.Name
		if list.Description != nil {
			text += " " + *list.Description
		}
		if list.Purpose != nil {
			text += " " + *list.Purpose
		}
	case "app.bsky.feed.generator":
		generator, ok := c.RecordOp.Value.(*appbsky.FeedGenerator)
		if !ok {
			return fmt.Errorf("mismatch between collection (%s) and type", c.RecordOp.Collection)
		}
		name += " " + generator.DisplayName
		if generator.Description != nil {
			text += " " + *generator.Description
		}
	}
	if name != "" {
		// check for explicit slurs or bad word tokens
		word := keyword.SlugContainsExplicitSlur(keyword.Slugify(name))
		if word != "" {
			c.AddRecordFlag("explicit-slur-name")
		}
		tokens := keyword.TokenizeText(name)
		for _, tok := range tokens {
			if c.InSet("bad-words", tok) {
				c.AddRecordFlag("bad-word-text")
				break
			}
		}
	}
	if text != "" {
		// check for explicit slurs or worst word tokens
		word := keyword.SlugContainsExplicitSlur(keyword.Slugify(text))
		if word != "" {
			c.AddRecordFlag("explicit-slur-text")
		}
		tokens := keyword.TokenizeText(text)
		for _, tok := range tokens {
			if c.InSet("worst-words", tok) {
				c.AddRecordFlag("bad-word-text")
				break
			}
		}
	}
	return nil
}

var _ automod.RecordRuleFunc = BadWordOtherRecordRule

// scans the record-key for all records
func BadWordRecordKeyRule(c *automod.RecordContext) error {
	// check record key
	word := keyword.SlugContainsExplicitSlur(keyword.Slugify(c.RecordOp.RecordKey.String()))
	if word != "" {
		c.AddRecordFlag("bad-word-record-key")
	}
	tokens := keyword.TokenizeIdentifier(c.RecordOp.RecordKey.String())
	for _, tok := range tokens {
		if c.InSet("bad-words", tok) {
			c.AddRecordFlag("bad-word-record-key")
			break
		}
	}

	return nil
}

var _ automod.RecordRuleFunc = BadWordRecordKeyRule

func BadWordHandleRule(c *automod.AccountContext) error {
	word := keyword.SlugContainsExplicitSlur(keyword.Slugify(c.Account.Identity.Handle.String()))
	if word != "" {
		c.AddAccountFlag("bad-word-handle")
	}

	tokens := keyword.TokenizeIdentifier(c.Account.Identity.Handle.String())
	for _, tok := range tokens {
		if c.InSet("bad-words", tok) {
			c.AddAccountFlag("bad-word-handle")
			break
		}
	}

	return nil
}

var _ automod.IdentityRuleFunc = BadWordHandleRule
