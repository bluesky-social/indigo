package engine

import (
	appgndr "github.com/gander-social/gander-indigo-sovereign/api/gndr"
	lexutil "github.com/gander-social/gander-indigo-sovereign/lex/util"
)

type IdentityRuleFunc = func(c *AccountContext) error
type AccountRuleFunc = func(c *AccountContext) error
type RecordRuleFunc = func(c *RecordContext) error
type PostRuleFunc = func(c *RecordContext, post *appgndr.FeedPost) error
type ProfileRuleFunc = func(c *RecordContext, profile *appgndr.ActorProfile) error
type BlobRuleFunc = func(c *RecordContext, blob lexutil.LexBlob, data []byte) error
type OzoneEventRuleFunc = func(c *OzoneEventContext) error
