package automod

import (
	"context"
	"fmt"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
)

type RuleSet struct {
	PostRules         []PostRuleFunc
	ProfileRules      []ProfileRuleFunc
	RecordRules       []RecordRuleFunc
	RecordDeleteRules []RecordDeleteRuleFunc
	IdentityRules     []IdentityRuleFunc
}

func (r *RuleSet) CallRecordRules(ctx context.Context, evt *RecordEvent) error {
	// first the generic rules
	for _, f := range r.RecordRules {
		err := f(ctx, evt)
		if err != nil {
			return err
		}
		if evt.Err != nil {
			return evt.Err
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
			err := f(ctx, evt, post)
			if err != nil {
				return err
			}
			if evt.Err != nil {
				return evt.Err
			}
		}
	case "app.bsky.actor.profile":
		profile, ok := evt.Record.(*appbsky.ActorProfile)
		if !ok {
			return fmt.Errorf("mismatch between collection (%s) and type", evt.Collection)
		}
		for _, f := range r.ProfileRules {
			err := f(ctx, evt, profile)
			if err != nil {
				return err
			}
			if evt.Err != nil {
				return evt.Err
			}
		}
	}
	return nil
}

func (r *RuleSet) CallRecordDeleteRules(ctx context.Context, evt *RecordDeleteEvent) error {
	for _, f := range r.RecordDeleteRules {
		err := f(ctx, evt)
		if err != nil {
			return err
		}
		if evt.Err != nil {
			return evt.Err
		}
	}
	return nil
}

func (r *RuleSet) CallIdentityRules(ctx context.Context, evt *IdentityEvent) error {
	for _, f := range r.IdentityRules {
		err := f(ctx, evt)
		if err != nil {
			return err
		}
		if evt.Err != nil {
			return evt.Err
		}
	}
	return nil
}
