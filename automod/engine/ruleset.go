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
	BlobRules         []BlobRuleFunc
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
	switch c.RecordOp.Collection.String() {
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
	// then blob rules, if any
	if len(r.BlobRules) == 0 || c.RecordOp.Action != CreateOp {
		return nil
	}
	blobs, err := c.Blobs()
	if err != nil {
		// TODO: should this really return error, or just log?
		return err
	}
	if len(blobs) == 0 {
		return nil
	}
	// TODO: concurrency
	for _, blob := range blobs {
		data, err := fetchBlob(c, blob)
		if err != nil {
			return err
		}
		for _, f := range r.BlobRules {
			err := f(c, blob, data)
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
