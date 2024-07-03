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
			HarassmentTargetInteractionPostRule,
			HarassmentTrivialPostRule,
			NostrSpamPostRule,
		},
		ProfileRules: []automod.ProfileRuleFunc{
			GtubeProfileRule,
			BadWordProfileRule,
			BotLinkProfileRule,
			CelebSpamProfileRule,
		},
		RecordRules: []automod.RecordRuleFunc{
			InteractionChurnRule,
			BadWordRecordKeyRule,
			BadWordOtherRecordRule,
			TooManyRepostRule,
		},
		RecordDeleteRules: []automod.RecordRuleFunc{
			DeleteInteractionRule,
		},
		IdentityRules: []automod.IdentityRuleFunc{
			NewAccountRule,
			BadWordHandleRule,
			BadWordDIDRule,
			NewAccountBotEmailRule,
			CelebSpamIdentityRule,
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
