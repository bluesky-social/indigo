package engine

import (
	"fmt"
	"sync"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	lexutil "github.com/bluesky-social/indigo/lex/util"
)

// Holds configuration of which rules of various types should be run, and helps dispatch events to those rules.
type RuleSet struct {
	PostRules         []PostRuleFunc
	ProfileRules      []ProfileRuleFunc
	RecordRules       []RecordRuleFunc
	RecordDeleteRules []RecordRuleFunc
	IdentityRules     []IdentityRuleFunc
	BlobRules         []BlobRuleFunc
}

// Executes all the various record-related rules. Only dispatches execution, does no other de-dupe or pre/post processing.
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
	if len(r.BlobRules) == 0 {
		return nil
	}
	err := r.fetchAndProcessBlobs(c)
	if err != nil {
		return err
	}

	return nil
}

// NOTE: this will probably be removed and merged in to `CallRecordRules`
func (r *RuleSet) CallRecordDeleteRules(c *RecordContext) error {
	for _, f := range r.RecordDeleteRules {
		err := f(c)
		if err != nil {
			return err
		}
	}
	return nil
}

// Executes rules for identity update events.
func (r *RuleSet) CallIdentityRules(c *AccountContext) error {
	for _, f := range r.IdentityRules {
		err := f(c)
		if err != nil {
			return err
		}
	}
	return nil
}

// high-level helper for fetching and processing blobs concurrently
func (r *RuleSet) fetchAndProcessBlobs(c *RecordContext) error {

	blobs, err := c.Blobs()
	if err != nil {
		// TODO: should this really return error, or just log?
		return err
	}
	if len(blobs) == 0 {
		return nil
	}

	errChan := make(chan error, len(blobs))
	var wg sync.WaitGroup
	for _, blob := range blobs {
		wg.Add(1)
		go func(blob lexutil.LexBlob) {
			defer wg.Done()
			data, err := fetchBlob(c, blob)
			if err != nil {
				errChan <- err
				return
			}
			err = r.processBlob(c, blob, data)
			if err != nil {
				errChan <- err
				return
			}
		}(blob)

	}
	wg.Wait()
	close(errChan)

	// check for errors
	for err := range errChan {
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *RuleSet) processBlob(c *RecordContext, blob lexutil.LexBlob, data []byte) error {
	errChan := make(chan error, len(r.BlobRules))
	var wg sync.WaitGroup
	for _, f := range r.BlobRules {
		wg.Add(1)
		go func(brf BlobRuleFunc) {
			defer wg.Done()
			err := brf(c, blob, data)
			if err != nil {
				errChan <- err
				return
			}
		}(f)
	}

	wg.Wait()
	close(errChan)

	// check for errors
	for err := range errChan {
		if err != nil {
			return err
		}
	}
	return nil
}
