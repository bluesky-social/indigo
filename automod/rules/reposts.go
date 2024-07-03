package rules

import (
	"time"

	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/automod/countstore"
)

var repostDailyThreshold = 500

var _ automod.RecordRuleFunc = TooManyRepostRule

// looks for accounts which do frequent reposts
func TooManyRepostRule(c *automod.RecordContext) error {

	did := c.Account.Identity.DID.String()
	// Don't bother checking reposts from accounts older than 30 days
	if c.Account.Private != nil {
		age := time.Since(c.Account.Private.IndexedAt)
		if age > 30*24*time.Hour {
			return nil
		}
	}

	switch c.RecordOp.Collection {
	case "app.bsky.feed.post":
		c.Increment("post", did)
	case "app.bsky.feed.repost":
		c.Increment("repost", did)
		repostCount := c.GetCount("repost", did, countstore.PeriodDay)
		postCount := c.GetCount("post", did, countstore.PeriodDay)
		ratio := float64(repostCount) / float64(postCount)
		if repostCount > repostDailyThreshold && ratio > 0.8 {
			c.Logger.Info("high-repost-count", "reposted-today", repostCount, "posted-today", postCount)
			c.AddAccountFlag("high-repost-count")
			// c.ReportAccount(automod.ReportReasonSpam, fmt.Sprintf("too many reposts: %d reposts, %d posts today (so far)", repostCount, postCount))
			c.Notify("slack")
		}
	}
	return nil
}
