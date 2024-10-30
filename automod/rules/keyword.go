package rules

import (
	"bytes"
	"fmt"
	"strings"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/automod/helpers"
	"github.com/bluesky-social/indigo/automod/keyword"
)

func BadWordPostRule(c *automod.RecordContext, post *appbsky.FeedPost) error {
	isJapanese := false
	for _, lang := range post.Langs {
		if lang == "ja" || strings.HasPrefix(lang, "ja-") {
			isJapanese = true
		}
	}
	for _, tok := range helpers.ExtractTextTokensPost(post) {
		word := keyword.SlugIsExplicitSlur(tok)
		// used very frequently in a reclaimed context
		if word != "" && word != "faggot" && word != "tranny" && word != "coon" && !(word == "kike" && isJapanese) {
			c.AddRecordFlag("bad-word-text")
			c.ReportRecord(automod.ReportReasonRude, fmt.Sprintf("possible bad word in post text or alttext: %s", word))
			//c.Notify("slack")
			break
		}
		// de-pluralize
		tok = strings.TrimSuffix(tok, "s")
		if c.InSet("worst-words", tok) {
			// skip this specific term, if used in a Japanese language post
			if isJapanese && tok == "kike" {
				continue
			}

			c.AddRecordFlag("bad-word-text")
			c.ReportRecord(automod.ReportReasonRude, fmt.Sprintf("possible bad word in post text or alttext: %s", tok))
			//c.Notify("slack")
			break
		}
	}
	return nil
}

var _ automod.PostRuleFunc = BadWordPostRule

func BadWordProfileRule(c *automod.RecordContext, profile *appbsky.ActorProfile) error {
	if profile.DisplayName != nil {
		word := keyword.SlugContainsExplicitSlur(keyword.Slugify(*profile.DisplayName))
		if word != "" {
			c.AddRecordFlag("bad-word-name")
			c.ReportRecord(automod.ReportReasonRude, fmt.Sprintf("possible bad word in display name: %s", word))
			//c.Notify("slack")
		}
	}
	for _, tok := range helpers.ExtractTextTokensProfile(profile) {
		// de-pluralize
		tok = strings.TrimSuffix(tok, "s")
		if c.InSet("worst-words", tok) {
			c.AddRecordFlag("bad-word-text")
			c.ReportRecord(automod.ReportReasonRude, fmt.Sprintf("possible bad word in profile description: %s", tok))
			//c.Notify("slack")
			break
		}
	}
	return nil
}

var _ automod.ProfileRuleFunc = BadWordProfileRule

// looks for the specific harassment situation of a replay to another user with only a single word
func ReplySingleBadWordPostRule(c *automod.RecordContext, post *appbsky.FeedPost) error {
	if post.Reply != nil && !helpers.IsSelfThread(c, post) {
		tokens := helpers.ExtractTextTokensPost(post)
		if len(tokens) != 1 {
			return nil
		}
		tok := tokens[0]
		if c.InSet("bad-words", tok) || keyword.SlugIsExplicitSlur(tok) != "" {
			c.AddRecordFlag("reply-single-bad-word")
			c.ReportRecord(automod.ReportReasonRude, fmt.Sprintf("bad single-word reply: %s", tok))
			//c.Notify("slack")
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
		var list appbsky.GraphList
		if err := list.UnmarshalCBOR(bytes.NewReader(c.RecordOp.RecordCBOR)); err != nil {
			return fmt.Errorf("failed to parse app.bsky.graph.list record: %v", err)
		}
		name += " " + list.Name
		if list.Description != nil {
			text += " " + *list.Description
		}
		if list.Purpose != nil {
			text += " " + *list.Purpose
		}
	case "app.bsky.feed.generator":
		var generator appbsky.FeedGenerator
		if err := generator.UnmarshalCBOR(bytes.NewReader(c.RecordOp.RecordCBOR)); err != nil {
			return fmt.Errorf("failed to parse app.bsky.feed.generator record: %v", err)
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
			c.AddRecordFlag("bad-word-name")
			c.ReportRecord(automod.ReportReasonRude, fmt.Sprintf("possible bad word in name: %s", word))
			c.Notify("slack")
		}
		tokens := keyword.TokenizeText(name)
		for _, tok := range tokens {
			if c.InSet("bad-words", tok) {
				c.AddRecordFlag("bad-word-name")
				c.ReportRecord(automod.ReportReasonRude, fmt.Sprintf("possible bad word in name: %s", tok))
				c.Notify("slack")
				break
			}
		}
	}
	if text != "" {
		// check for explicit slurs or worst word tokens
		word := keyword.SlugContainsExplicitSlur(keyword.Slugify(text))
		if word != "" {
			c.AddRecordFlag("bad-word-text")
			c.ReportRecord(automod.ReportReasonRude, fmt.Sprintf("possible bad word in description: %s", word))
			c.Notify("slack")
		}
		tokens := keyword.TokenizeText(text)
		for _, tok := range tokens {
			// de-pluralize
			tok = strings.TrimSuffix(tok, "s")
			if c.InSet("worst-words", tok) {
				c.AddRecordFlag("bad-word-text")
				c.ReportRecord(automod.ReportReasonRude, fmt.Sprintf("possible bad word in description: %s", tok))
				c.Notify("slack")
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
	word := keyword.SlugIsExplicitSlur(keyword.Slugify(c.RecordOp.RecordKey.String()))
	if word != "" {
		c.AddRecordFlag("bad-word-recordkey")
		c.ReportRecord(automod.ReportReasonRude, fmt.Sprintf("possible bad word in record-key (URL): %s", word))
		c.Notify("slack")
	}
	tokens := keyword.TokenizeIdentifier(c.RecordOp.RecordKey.String())
	for _, tok := range tokens {
		if c.InSet("bad-words", tok) {
			c.AddRecordFlag("bad-word-recordkey")
			c.ReportRecord(automod.ReportReasonRude, fmt.Sprintf("possible bad word in record-key (URL): %s", tok))
			c.Notify("slack")
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
		c.ReportAccount(automod.ReportReasonRude, fmt.Sprintf("possible bad word in handle (username): %s", word))
		//c.Notify("slack")
		return nil
	}

	tokens := keyword.TokenizeIdentifier(c.Account.Identity.Handle.String())
	for _, tok := range tokens {
		if c.InSet("bad-words", tok) {
			c.AddAccountFlag("bad-word-handle")
			c.ReportAccount(automod.ReportReasonRude, fmt.Sprintf("possible bad word in handle (username): %s", tok))
			//c.Notify("slack")
			break
		}
	}

	return nil
}

var _ automod.IdentityRuleFunc = BadWordHandleRule

func BadWordDIDRule(c *automod.AccountContext) error {
	if c.Account.Identity.DID.Method() == "plc" {
		return nil
	}
	word := keyword.SlugContainsExplicitSlur(keyword.Slugify(c.Account.Identity.DID.String()))
	if word != "" {
		c.AddAccountFlag("bad-word-did")
		c.ReportAccount(automod.ReportReasonRude, fmt.Sprintf("possible bad word in DID (account identifier): %s", word))
		c.Notify("slack")
		return nil
	}

	tokens := keyword.TokenizeIdentifier(c.Account.Identity.DID.String())
	for _, tok := range tokens {
		if c.InSet("bad-words", tok) {
			c.AddAccountFlag("bad-word-did")
			c.ReportAccount(automod.ReportReasonRude, fmt.Sprintf("possible bad word in DID (account identifier): %s", tok))
			c.Notify("slack")
			break
		}
	}

	return nil
}

var _ automod.IdentityRuleFunc = BadWordDIDRule
