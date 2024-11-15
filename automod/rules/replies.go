package rules

import (
	"fmt"
	"time"
	"unicode/utf8"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/automod/countstore"
	"github.com/bluesky-social/indigo/automod/helpers"
)

var _ automod.PostRuleFunc = ReplyCountPostRule

// does not count "self-replies" (direct to self, or in own post thread)
func ReplyCountPostRule(c *automod.RecordContext, post *appbsky.FeedPost) error {
	if post.Reply == nil || helpers.IsSelfThread(c, post) {
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

// triggers on the N+1 post
// var identicalReplyLimit = 6
// TODO: bumping temporarily
var identicalReplyLimit = 20
var identicalReplyActionLimit = 75

var _ automod.PostRuleFunc = IdenticalReplyPostRule

// Looks for accounts posting the exact same text multiple times. Does not currently count the number of distinct accounts replied to, just counts replies at all.
//
// There can be legitimate situations that trigger this rule, so in most situations should be a "report" not "label" action.
func IdenticalReplyPostRule(c *automod.RecordContext, post *appbsky.FeedPost) error {
	if post.Reply == nil || helpers.IsSelfThread(c, post) {
		return nil
	}

	// don't action short replies, or accounts more than two weeks old
	if utf8.RuneCountInString(post.Text) <= 10 {
		return nil
	}
	if helpers.AccountIsOlderThan(&c.AccountContext, 14*24*time.Hour) {
		return nil
	}

	// don't count if there is a follow-back relationship
	if helpers.ParentOrRootIsFollower(c, post) {
		return nil
	}

	// increment before read. use a specific period (IncrementPeriod()) to reduce the number of counters (one per unique post text)
	period := countstore.PeriodDay
	bucket := c.Account.Identity.DID.String() + "/" + helpers.HashOfString(post.Text)
	c.IncrementPeriod("reply-text", bucket, period)

	count := c.GetCount("reply-text", bucket, period)
	if count >= identicalReplyLimit {
		c.AddAccountFlag("multi-identical-reply")
		c.ReportAccount(automod.ReportReasonSpam, fmt.Sprintf("possible spam (new account, %d identical reply-posts today)", count))
		c.Notify("slack")
	}
	if count >= identicalReplyActionLimit && utf8.RuneCountInString(post.Text) > 100 {
		c.ReportAccount(automod.ReportReasonRude, fmt.Sprintf("likely spam/harassment (new account, %d identical reply-posts today), actioned (remove label urgently if account is ok)", count))
		c.AddAccountLabel("!warn")
		c.Notify("slack")
	}

	return nil
}

// Similar to above rule but only counts replies to the same post. More aggressively applies a spam label to new accounts that are less than a day old.
var identicalReplySameParentLimit = 3
var identicalReplySameParentMaxAge = 24 * time.Hour
var identicalReplySameParentMaxPosts int64 = 50
var _ automod.PostRuleFunc = IdenticalReplyPostSameParentRule

func IdenticalReplyPostSameParentRule(c *automod.RecordContext, post *appbsky.FeedPost) error {
	if post.Reply == nil || helpers.IsSelfThread(c, post) {
		return nil
	}

	if helpers.ParentOrRootIsFollower(c, post) {
		return nil
	}

	postCount := c.Account.PostsCount
	if helpers.AccountIsOlderThan(&c.AccountContext, identicalReplySameParentMaxAge) || postCount >= identicalReplySameParentMaxPosts {
		return nil
	}

	period := countstore.PeriodHour
	bucket := c.Account.Identity.DID.String() + "/" + post.Reply.Parent.Uri + "/" + helpers.HashOfString(post.Text)
	c.IncrementPeriod("reply-text-same-post", bucket, period)

	count := c.GetCount("reply-text-same-post", bucket, period)
	if count >= identicalReplySameParentLimit {
		c.AddAccountFlag("multi-identical-reply-same-post")
		c.ReportAccount(automod.ReportReasonSpam, fmt.Sprintf("possible spam (%d identical reply-posts to same post today)", count))
		c.AddAccountLabel("spam")
		c.Notify("slack")
	}

	return nil
}

// TODO: bumping temporarily
// var youngReplyAccountLimit = 12
var youngReplyAccountLimit = 200
var _ automod.PostRuleFunc = YoungAccountDistinctRepliesRule

func YoungAccountDistinctRepliesRule(c *automod.RecordContext, post *appbsky.FeedPost) error {
	// only replies, and skip self-replies (eg, threads)
	if post.Reply == nil || helpers.IsSelfThread(c, post) {
		return nil
	}

	// don't action short replies, or accounts more than two weeks old
	if utf8.RuneCountInString(post.Text) <= 10 {
		return nil
	}
	if helpers.AccountIsOlderThan(&c.AccountContext, 14*24*time.Hour) {
		return nil
	}

	// don't count if there is a follow-back relationship
	if helpers.ParentOrRootIsFollower(c, post) {
		return nil
	}

	parentURI, err := syntax.ParseATURI(post.Reply.Parent.Uri)
	if err != nil {
		c.Logger.Warn("failed to parse reply AT-URI", "uri", post.Reply.Parent.Uri)
		return nil
	}
	parentDID, err := parentURI.Authority().AsDID()
	if err != nil {
		c.Logger.Warn("reply AT-URI authority not a DID", "uri", post.Reply.Parent.Uri)
		return nil
	}

	did := c.Account.Identity.DID.String()

	c.IncrementDistinct("young-reply-to", did, parentDID.String())
	// NOTE: won't include the increment from this event
	count := c.GetCountDistinct("young-reply-to", did, countstore.PeriodHour)
	if count >= youngReplyAccountLimit {
		c.AddAccountFlag("new-account-distinct-account-reply")
		c.ReportAccount(automod.ReportReasonRude, fmt.Sprintf("possible spam (new account, reply-posts to %d distinct accounts in past hour)", count))
		c.Notify("slack")
	}

	return nil
}
