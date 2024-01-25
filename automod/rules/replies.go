package rules

import (
	"time"
	"unicode/utf8"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/automod/countstore"
)

var _ automod.PostRuleFunc = ReplyCountPostRule

// does not count "self-replies" (direct to self, or in own post thread)
func ReplyCountPostRule(c *automod.RecordContext, post *appbsky.FeedPost) error {
	if post.Reply == nil || IsSelfThread(c, post) {
		return nil
	}

	did := c.Account.Identity.DID.String()
	if c.GetCount("reply", did, countstore.PeriodDay) > 3 {
		// TODO: disabled, too noisy for prod
		//c.AddAccountFlag("frequent-replier")
	}
	c.Increment("reply", did)

	parentURI, err := syntax.ParseATURI(post.Reply.Parent.Uri)
	if err != nil {
		c.Logger.Warn("failed to parse reply AT-URI", "uri", post.Reply.Parent.Uri)
		return nil
	}
	c.IncrementDistinct("reply-to", did, parentURI.Authority().String())
	return nil
}

// triggers on the N+1 post, so 6th identical reply
var identicalReplyLimit = 5

var _ automod.PostRuleFunc = IdenticalReplyPostRule

// Looks for accounts posting the exact same text multiple times. Does not currently count the number of distinct accounts replied to, just counts replies at all.
//
// There can be legitimate situations that trigger this rule, so in most situations should be a "report" not "label" action.
func IdenticalReplyPostRule(c *automod.RecordContext, post *appbsky.FeedPost) error {
	if post.Reply == nil || IsSelfThread(c, post) {
		return nil
	}

	// increment first. use a specific period (IncrementPeriod()) to reduce the number of counters (one per unique post text)
	period := countstore.PeriodDay
	bucket := c.Account.Identity.DID.String() + "/" + HashOfString(post.Text)
	c.IncrementPeriod("reply-text", bucket, period)

	// don't action short replies, or accounts more than two weeks old
	if utf8.RuneCountInString(post.Text) <= 10 {
		return nil
	}
	if c.Account.Private != nil {
		age := time.Since(c.Account.Private.IndexedAt)
		if age > 2*7*24*time.Hour {
			return nil
		}
	}

	if c.GetCount("reply-text", bucket, period) >= identicalReplyLimit {
		c.AddAccountFlag("multi-identical-reply")
		c.Notify("slack")
	}

	return nil
}
