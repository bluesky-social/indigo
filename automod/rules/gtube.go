package rules

import (
	"strings"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/automod"
)

// https://en.wikipedia.org/wiki/GTUBE
var gtubeString = "XJS*C4JDBQADN1.NSBN3*2IDNEN*GTUBE-STANDARD-ANTI-UBE-TEST-EMAIL*C.34X"

var _ automod.PostRuleFunc = GtubePostRule

func GtubePostRule(c *automod.RecordContext, post *appbsky.FeedPost) error {
	if strings.Contains(post.Text, gtubeString) {
		c.AddRecordLabel("spam")
		c.Notify("slack")
		c.AddRecordTag("gtube-record")
	}
	return nil
}

var _ automod.ProfileRuleFunc = GtubeProfileRule

func GtubeProfileRule(c *automod.RecordContext, profile *appbsky.ActorProfile) error {
	if profile.Description != nil && strings.Contains(*profile.Description, gtubeString) {
		c.AddRecordLabel("spam")
		c.Notify("slack")
		c.AddAccountTag("gtuber-account")
	}
	return nil
}
