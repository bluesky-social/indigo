package rules

import (
	"github.com/bluesky-social/indigo/automod"
)

func DefaultRules() automod.RuleSet {
	rules := automod.RuleSet{
		PostRules: []automod.PostRuleFunc{
			MisleadingURLPostRule,
			MisleadingMentionPostRule,
			ReplyCountPostRule,
			BadHashtagsPostRule,
			AccountDemoPostRule,
			AccountPrivateDemoPostRule,
		},
	}
	return rules
}
