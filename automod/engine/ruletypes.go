package engine

import (
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	lexutil "github.com/bluesky-social/indigo/lex/util"
)

type IdentityRuleFunc = func(c *AccountContext) error
type AccountRuleFunc = func(c *AccountContext) error
type RecordRuleFunc = func(c *RecordContext) error
type PostRuleFunc = func(c *RecordContext, post *appbsky.FeedPost) error
type ProfileRuleFunc = func(c *RecordContext, profile *appbsky.ActorProfile) error
type BlobRuleFunc = func(c *RecordContext, blob lexutil.LexBlob, data []byte) error
type OzoneEventRuleFunc = func(c *OzoneEventContext) error
