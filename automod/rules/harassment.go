package rules

import (
	"fmt"
	"time"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/automod/countstore"
	"github.com/bluesky-social/indigo/automod/helpers"
)

var _ automod.PostRuleFunc = HarassmentTargetInteractionPostRule

// looks for new accounts, which interact with frequently-harassed accounts, and report them for review
func HarassmentTargetInteractionPostRule(c *automod.RecordContext, post *appbsky.FeedPost) error {
	if c.Account.Identity == nil || !helpers.AccountIsYoungerThan(&c.AccountContext, 24*time.Hour) {
		return nil
	}

	var interactionDIDs []string
	facets, err := helpers.ExtractFacets(post)
	if err != nil {
		return err
	}
	for _, pf := range facets {
		if pf.DID != nil {
			interactionDIDs = append(interactionDIDs, *pf.DID)
		}
	}
	if post.Reply != nil && !helpers.IsSelfThread(c, post) {
		parentURI, err := syntax.ParseATURI(post.Reply.Parent.Uri)
		if err != nil {
			return err
		}
		interactionDIDs = append(interactionDIDs, parentURI.Authority().String())
	}
	// quote posts
	if post.Embed != nil && post.Embed.EmbedRecord != nil && post.Embed.EmbedRecord.Record != nil {
		uri, err := syntax.ParseATURI(post.Embed.EmbedRecord.Record.Uri)
		if err != nil {
			c.Logger.Warn("invalid AT-URI in post embed record (quote-post)", "uri", post.Embed.EmbedRecord.Record.Uri)
		} else {
			interactionDIDs = append(interactionDIDs, uri.Authority().String())
		}
	}
	if len(interactionDIDs) == 0 {
		return nil
	}

	// more than a handful of followers or posts from author account? skip
	if c.Account.FollowersCount > 10 || c.Account.PostsCount > 10 {
		return nil
	}
	postCount := c.GetCount("post", c.Account.Identity.DID.String(), countstore.PeriodTotal)
	if postCount > 20 {
		return nil
	}

	interactionDIDs = helpers.DedupeStrings(interactionDIDs)
	for _, d := range interactionDIDs {
		did, err := syntax.ParseDID(d)
		if err != nil {
			c.Logger.Warn("invalid DID in record", "did", d)
			continue
		}
		if did == c.Account.Identity.DID {
			continue
		}
		targetIsProtected := false
		if c.InSet("harassment-target-dids", did.String()) {
			targetIsProtected = true
		} else {
			// check if the target account has a harassment protection tag in Ozone
			targetAccount := c.GetAccountMeta(did)
			if targetAccount == nil {
				continue
			}
			if targetAccount.Private != nil {
				for _, t := range targetAccount.Private.AccountTags {
					if t == "harassment-protection" {
						targetIsProtected = true
						break
					}
				}
			}
		}

		if !targetIsProtected {
			continue
		}

		// ignore if the target account follows the new account
		rel := c.GetAccountRelationship(syntax.DID(did))
		if rel.FollowedBy {
			continue
		}

		//c.AddRecordFlag("interaction-harassed-target")
		var privCreatedAt *time.Time
		if c.Account.Private != nil && c.Account.Private.IndexedAt != nil {
			privCreatedAt = c.Account.Private.IndexedAt
		}
		c.Logger.Warn("possible harassment", "targetDID", did, "author", c.Account.Identity.DID, "accountCreated", c.Account.CreatedAt, "privateAccountCreated", privCreatedAt)
		c.ReportAccount(automod.ReportReasonOther, fmt.Sprintf("possible harassment of known target account: %s (also labeled; remove label if this isn't harassment)", did))
		c.AddAccountLabel("!hide")
		c.Notify("slack")
		return nil
	}
	return nil
}

var _ automod.PostRuleFunc = HarassmentTrivialPostRule

// looks for new accounts, which frequently post the same type of content
func HarassmentTrivialPostRule(c *automod.RecordContext, post *appbsky.FeedPost) error {
	if c.Account.Identity == nil || !helpers.AccountIsYoungerThan(&c.AccountContext, 7*24*time.Hour) {
		return nil
	}

	// only posts with dumb pattern
	if post.Text != "F" {
		return nil
	}

	did := c.Account.Identity.DID.String()
	c.Increment("trivial-harassing", did)
	count := c.GetCount("trivial-harassing", did, countstore.PeriodDay)

	if count > 5 {
		//c.AddRecordFlag("trivial-harassing-post")
		c.ReportAccount(automod.ReportReasonOther, "possible targetted harassment (also labeled; remove label if this isn't harassment!)")
		c.AddAccountLabel("!hide")
		c.Notify("slack")
	}
	return nil
}

var _ automod.OzoneEventRuleFunc = HarassmentProtectionOzoneEventRule

// looks for new harassment protection tags on accounts, and logs them
func HarassmentProtectionOzoneEventRule(c *automod.OzoneEventContext) error {
	if c.Event.EventType != "tag" || c.Event.Event.ModerationDefs_ModEventTag == nil {
		return nil
	}

	for _, t := range c.Event.Event.ModerationDefs_ModEventTag.Add {
		if t == "harassment-protection" {
			c.Logger.Info("adding harassment protection to account", "ozoneComment", c.Event.Event.ModerationDefs_ModEventTag.Comment, "did", c.Account.Identity.DID, "handle", c.Account.Identity.Handle)
			// to make slack message clearer; bluring flags and tags is a bit weird
			c.AddAccountFlag("harassment-protection")
			//c.Notify("slack")
			break
		}
	}
	return nil
}
