package rules

import (
	"github.com/bluesky-social/indigo/automod"
)

// IMPORTANT: reminder that these are the indigo-edition rules, not production rules
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
			//IdenticalReplyPostSameParentRule,
			DistinctMentionsRule,
			YoungAccountDistinctMentionsRule,
			MisleadingLinkUnicodeReversalPostRule,
			SimpleBotPostRule,
			HarassmentTargetInteractionPostRule,
			HarassmentTrivialPostRule,
			NostrSpamPostRule,
			TrivialSpamPostRule,
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
		OzoneEventRules: []automod.OzoneEventRuleFunc{
			HarassmentProtectionOzoneEventRule,
		},
	}
	return rules
}
