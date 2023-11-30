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
			//TooManyHashtagsPostRule,
			AccountDemoPostRule,
			AccountPrivateDemoPostRule,
			GtubePostRule,
			KeywordPostRule,
			ReplySingleKeywordPostRule,
		},
		ProfileRules: []automod.ProfileRuleFunc{
			GtubeProfileRule,
			KeywordProfileRule,
		},
		IdentityRules: []automod.IdentityRuleFunc{
			NoOpIdentityRule,
		},
	}
	return rules
}
