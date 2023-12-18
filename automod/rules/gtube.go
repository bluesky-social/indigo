package rules

import (
	"context"
	"strings"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/automod"
)

var (
	_ automod.PostRuleFunc    = GtubePostRule
	_ automod.ProfileRuleFunc = GtubeProfileRule
)

// https://en.wikipedia.org/wiki/GTUBE
var gtubeString = "XJS*C4JDBQADN1.NSBN3*2IDNEN*GTUBE-STANDARD-ANTI-UBE-TEST-EMAIL*C.34X"

func GtubePostRule(ctx context.Context, evt *automod.RecordEvent, post *appbsky.FeedPost) error {
	if strings.Contains(post.Text, gtubeString) {
		evt.AddRecordLabel("spam")
	}
	return nil
}

func GtubeProfileRule(ctx context.Context, evt *automod.RecordEvent, profile *appbsky.ActorProfile) error {
	if profile.Description != nil && strings.Contains(*profile.Description, gtubeString) {
		evt.AddRecordLabel("spam")
	}
	return nil
}
