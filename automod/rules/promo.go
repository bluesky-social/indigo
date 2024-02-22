package rules

import (
	"net/url"
	"strings"
	"time"
	"fmt"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/automod/countstore"
)

var _ automod.PostRuleFunc = AggressivePromotionRule

// looks for new accounts, with a commercial or donation link in profile, which directly reply to several accounts
//
// this rule depends on ReplyCountPostRule() to set counts
func AggressivePromotionRule(c *automod.RecordContext, post *appbsky.FeedPost) error {
	if c.Account.Private == nil || c.Account.Identity == nil {
		return nil
	}
	// TODO: helper for account age
	age := time.Since(c.Account.Private.IndexedAt)
	if age > 7*24*time.Hour {
		return nil
	}
	if post.Reply == nil || IsSelfThread(c, post) {
		return nil
	}

	allURLs := ExtractTextURLs(post.Text)
	if c.Account.Profile.Description != nil {
		profileURLs := ExtractTextURLs(*c.Account.Profile.Description)
		allURLs = append(allURLs, profileURLs...)
	}
	hasPromo := false
	for _, s := range allURLs {
		if !strings.Contains(s, "://") {
			s = "https://" + s
		}
		u, err := url.Parse(s)
		if err != nil {
			c.Logger.Warn("failed to parse URL", "url", s)
			continue
		}
		host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
		if c.InSet("promo-domain", host) {
			hasPromo = true
			break
		}
	}
	if !hasPromo {
		return nil
	}

	did := c.Account.Identity.DID.String()
	uniqueReplies := c.GetCountDistinct("reply-to", did, countstore.PeriodDay)
	if uniqueReplies >= 10 {
		c.AddAccountFlag("promo-multi-reply")
		c.ReportAccount(automod.ReportReasonSpam, fmt.Sprintf("possible aggressive self-promotion"))
		c.Notify("slack")
	}

	return nil
}
