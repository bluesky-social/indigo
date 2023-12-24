package engine

import (
	appbsky "github.com/bluesky-social/indigo/api/bsky"
)

type IdentityRuleFunc = func(evt *IdentityEvent, eff *Effects) error
type RecordRuleFunc = func(evt *RecordEvent, eff *Effects) error
type PostRuleFunc = func(evt *RecordEvent, eff *Effects, post *appbsky.FeedPost) error
type ProfileRuleFunc = func(evt *RecordEvent, eff *Effects, profile *appbsky.ActorProfile) error
type RecordDeleteRuleFunc = func(evt *RecordDeleteEvent, eff *Effects) error
