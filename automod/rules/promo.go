package rules

import (
	"net/url"
	"strings"
	"time"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/automod/countstore"
)

// looks for new accounts, with a commercial or donation link in profile, which directly reply to several accounts
//
// this rule depends on ReplyCountPostRule() to set counts
func AggressivePromotionRule(evt *automod.RecordEvent, post *appbsky.FeedPost) error {
	if evt.Account.Private == nil || evt.Account.Identity == nil {
		return nil
	}
	// TODO: helper for account age
	age := time.Since(evt.Account.Private.IndexedAt)
	if age > 7*24*time.Hour {
		return nil
	}
	if post.Reply == nil || IsSelfThread(evt, post) {
		return nil
	}

	allURLs := ExtractTextURLs(post.Text)
	if evt.Account.Profile.Description != nil {
		profileURLs := ExtractTextURLs(*evt.Account.Profile.Description)
		allURLs = append(allURLs, profileURLs...)
	}
	hasPromo := false
	for _, s := range allURLs {
		if !strings.Contains(s, "://") {
			s = "https://" + s
		}
		u, err := url.Parse(s)
		if err != nil {
			evt.Logger.Warn("failed to parse URL", "url", s)
			continue
		}
		host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
		if evt.InSet("promo-domain", host) {
			hasPromo = true
			break
		}
	}
	if !hasPromo {
		return nil
	}

	did := evt.Account.Identity.DID.String()
	uniqueReplies := evt.GetCountDistinct("reply-to", did, countstore.PeriodDay)
	if uniqueReplies >= 5 {
		evt.AddAccountFlag("promo-multi-reply")
	}

	return nil
}
