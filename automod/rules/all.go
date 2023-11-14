package rules

import (
	"github.com/bluesky-social/indigo/automod"
)

func DefaultRules() automod.RuleSet {
	rules := automod.RuleSet{
		PostRules: []automod.PostRuleFunc{
			BanHashtagsPostRule,
		},
	}
	return rules
}
