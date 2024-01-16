package rules

import (
	"github.com/bluesky-social/indigo/automod"
)

func DefaultRules() automod.RuleSet {
	rules := automod.RuleSet{
		PostRules: []automod.PostRuleFunc{
			//MisleadingURLPostRule,
			//MisleadingMentionPostRule,
			ReplyCountPostRule,
			BadHashtagsPostRule,
			//TooManyHashtagsPostRule,
			//AccountDemoPostRule,
			AccountPrivateDemoPostRule,
			GtubePostRule,
			BadWordPostRule,
			ReplySingleBadWordPostRule,
			AggressivePromotionRule,
			IdenticalReplyPostRule,
			DistinctMentionsRule,
			MisleadingLinkUnicodeReversalPostRule,
		},
		ProfileRules: []automod.ProfileRuleFunc{
			GtubeProfileRule,
			BadWordProfileRule,
		},
		RecordRules: []automod.RecordRuleFunc{
			InteractionChurnRule,
			RecordKeyBadWordRecordRule,
		},
		RecordDeleteRules: []automod.RecordRuleFunc{
			DeleteInteractionRule,
		},
		IdentityRules: []automod.IdentityRuleFunc{
			NewAccountRule,
			HandleBadWordIdentityRule,
		},
		BlobRules: []automod.BlobRuleFunc{
			//BlobVerifyRule,
		},
		NotificationRules: []automod.NotificationRuleFunc{
			// none
		},
	}
	return rules
}
