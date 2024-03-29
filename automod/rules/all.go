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
			YoungAccountDistinctRepliesRule,
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
			YoungAccountDistinctMentionsRule,
			MisleadingLinkUnicodeReversalPostRule,
			SimpleBotPostRule,
		},
		ProfileRules: []automod.ProfileRuleFunc{
			GtubeProfileRule,
			BadWordProfileRule,
			BotLinkProfileRule,
		},
		RecordRules: []automod.RecordRuleFunc{
			InteractionChurnRule,
			BadWordRecordKeyRule,
			BadWordOtherRecordRule,
		},
		RecordDeleteRules: []automod.RecordRuleFunc{
			DeleteInteractionRule,
		},
		IdentityRules: []automod.IdentityRuleFunc{
			NewAccountRule,
			BadWordHandleRule,
			BadWordDIDRule,
			NewAccountBotEmailRule,
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
