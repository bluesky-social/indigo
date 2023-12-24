package engine

import (
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/automod/effects"
	"github.com/bluesky-social/indigo/automod/event"
)

type IdentityRuleFunc = func(evt *event.IdentityEvent, eff *effects.Effects) error
type RecordRuleFunc = func(evt *event.RecordEvent, eff *effects.Effects) error
type PostRuleFunc = func(evt *event.RecordEvent, eff *effects.Effects, post *appbsky.FeedPost) error
type ProfileRuleFunc = func(evt *event.RecordEvent, eff *effects.Effects, profile *appbsky.ActorProfile) error
type RecordDeleteRuleFunc = func(evt *event.RecordDeleteEvent, eff *effects.Effects) error
