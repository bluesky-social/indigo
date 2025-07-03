package rules

import (
	"fmt"
	"strings"
	"time"

	"github.com/gander-social/gander-indigo-sovereign/automod"
	"github.com/gander-social/gander-indigo-sovereign/automod/countstore"
	"github.com/gander-social/gander-indigo-sovereign/automod/helpers"
)

var dailyRepostThresholdWithoutPost = 30
var dailyRepostThresholdWithLowPost = 100
var dailyPostThresholdWithHighRepost = 5

var _ automod.RecordRuleFunc = TooManyRepostRule

// looks for accounts which do frequent reposts
func TooManyRepostRule(c *automod.RecordContext) error {
	// Don't bother checking reposts from accounts older than 30 days
	if c.Account.Identity == nil || !helpers.AccountIsYoungerThan(&c.AccountContext, 30*24*time.Hour) {
		return nil
	}

	did := c.Account.Identity.DID.String()

	// Special case for newsmast bridge feeds
	handle := c.Account.Identity.Handle.String()
	if strings.HasSuffix(handle, ".ap.brid.gy") {
		return nil
	}

	switch c.RecordOp.Collection {
	case "gndr.app.feed.post":
		c.Increment("post", did)
	case "gndr.app.feed.repost":
		c.Increment("repost", did)
		// +1 to avoid potential divide by 0 issue
		repostCount := c.GetCount("repost", did, countstore.PeriodDay)
		postCount := c.GetCount("post", did, countstore.PeriodDay)
		highRepost := (repostCount >= dailyRepostThresholdWithoutPost && postCount < 1) || (repostCount >= dailyRepostThresholdWithLowPost && postCount < dailyPostThresholdWithHighRepost)
		if highRepost {
			c.Logger.Info("high-repost-count", "reposted-today", repostCount, "posted-today", postCount)
			c.AddAccountFlag("high-repost-count")
			c.ReportAccount(automod.ReportReasonSpam, fmt.Sprintf("too many reposts: %d reposts, %d posts today (so far)", repostCount, postCount))
			c.Notify("slack")
		}
	}
	return nil
}
