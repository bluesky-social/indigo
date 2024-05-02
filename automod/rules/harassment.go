package rules

import (
	"fmt"
	"time"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod"
)

var _ automod.PostRuleFunc = HarassmentTargetInteractionPostRule

// looks for new accounts, which interact with frequently-harassed accounts, and report them for review
func HarassmentTargetInteractionPostRule(c *automod.RecordContext, post *appbsky.FeedPost) error {
	if c.Account.Private == nil || c.Account.Identity == nil {
		return nil
	}
	// TODO: helper for account age; and use public info for this (not private)
	age := time.Since(c.Account.Private.IndexedAt)
	if age > 7*24*time.Hour {
		return nil
	}

	var interactionDIDs []string
	facets, err := ExtractFacets(post)
	if err != nil {
		return err
	}
	for _, pf := range facets {
		if pf.DID != nil {
			interactionDIDs = append(interactionDIDs, *pf.DID)
		}
	}
	if post.Reply != nil && !IsSelfThread(c, post) {
		parentURI, err := syntax.ParseATURI(post.Reply.Parent.Uri)
		if err != nil {
			return err
		}
		interactionDIDs = append(interactionDIDs, parentURI.Authority().String())
	}
	// TODO: quote-posts; any other interactions?
	if len(interactionDIDs) == 0 {
		return nil
	}

	interactionDIDs = dedupeStrings(interactionDIDs)
	for _, did := range interactionDIDs {
		if did == c.Account.Identity.DID.String() {
			continue
		}
		if c.InSet("harassment-target-dids", did) {
			//c.AddRecordFlag("interaction-harassed-target")
			c.ReportAccount(automod.ReportReasonOther, fmt.Sprintf("possible harassment of known target account: %s (also labeled; remove label if this isn't harassment)", did))
			c.AddAccountLabel("!hide")
			c.Notify("slack")
			return nil
		}
	}
	return nil
}
