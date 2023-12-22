package engine

import (
	"fmt"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/automod/effects"
	"github.com/bluesky-social/indigo/automod/event"
)

type RuleSet struct {
	PostRules         []PostRuleFunc
	ProfileRules      []ProfileRuleFunc
	RecordRules       []RecordRuleFunc
	RecordDeleteRules []RecordDeleteRuleFunc
	IdentityRules     []IdentityRuleFunc
}

func (r *RuleSet) CallRecordRules(evt *event.RecordEvent, eff *effects.RecordEffect) error {
	// first the generic rules
	for _, f := range r.RecordRules {
		err := f(evt, eff)
		if err != nil {
			return err
		}
	}
	// then any record-type-specific rules
	switch evt.Collection {
	case "app.bsky.feed.post":
		post, ok := evt.Record.(*appbsky.FeedPost)
		if !ok {
			return fmt.Errorf("mismatch between collection (%s) and type", evt.Collection)
		}
		for _, f := range r.PostRules {
			err := f(evt, eff, post)
			if err != nil {
				return err
			}
		}
	case "app.bsky.actor.profile":
		profile, ok := evt.Record.(*appbsky.ActorProfile)
		if !ok {
			return fmt.Errorf("mismatch between collection (%s) and type", evt.Collection)
		}
		for _, f := range r.ProfileRules {
			err := f(evt, eff, profile)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *RuleSet) CallRecordDeleteRules(evt *event.RecordDeleteEvent, eff *effects.RecordDeleteEffect) error {
	for _, f := range r.RecordDeleteRules {
		err := f(evt, eff)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *RuleSet) CallIdentityRules(evt *event.IdentityEvent, eff *effects.IdentityEffect) error {
	for _, f := range r.IdentityRules {
		err := f(evt, eff)
		if err != nil {
			return err
		}
	}
	return nil
}
