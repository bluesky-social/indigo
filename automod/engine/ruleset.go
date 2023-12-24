package engine

import (
	"fmt"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
)

type RuleSet struct {
	PostRules         []PostRuleFunc
	ProfileRules      []ProfileRuleFunc
	RecordRules       []RecordRuleFunc
	RecordDeleteRules []RecordRuleFunc
	IdentityRules     []IdentityRuleFunc
}

func (r *RuleSet) CallRecordRules(c *RecordContext) error {
	// first the generic rules
	for _, f := range r.RecordRules {
		err := f(c)
		if err != nil {
			return err
		}
	}
	// then any record-type-specific rules
	switch c.RecordOp.Collection {
	case "app.bsky.feed.post":
		post, ok := c.RecordOp.Value.(*appbsky.FeedPost)
		if !ok {
			return fmt.Errorf("mismatch between collection (%s) and type", c.RecordOp.Collection)
		}
		for _, f := range r.PostRules {
			err := f(c, post)
			if err != nil {
				return err
			}
		}
	case "app.bsky.actor.profile":
		profile, ok := c.RecordOp.Value.(*appbsky.ActorProfile)
		if !ok {
			return fmt.Errorf("mismatch between collection (%s) and type", c.RecordOp.Collection)
		}
		for _, f := range r.ProfileRules {
			err := f(c, profile)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *RuleSet) CallRecordDeleteRules(c *RecordContext) error {
	for _, f := range r.RecordDeleteRules {
		err := f(c)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *RuleSet) CallIdentityRules(c *AccountContext) error {
	for _, f := range r.IdentityRules {
		err := f(c)
		if err != nil {
			return err
		}
	}
	return nil
}
