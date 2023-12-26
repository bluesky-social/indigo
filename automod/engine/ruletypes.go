package engine

import (
	appbsky "github.com/bluesky-social/indigo/api/bsky"
)

type IdentityRuleFunc = func(c *AccountContext) error
type RecordRuleFunc = func(c *RecordContext) error
type PostRuleFunc = func(c *RecordContext, post *appbsky.FeedPost) error
type ProfileRuleFunc = func(c *RecordContext, profile *appbsky.ActorProfile) error
