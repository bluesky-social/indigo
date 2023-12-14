package rules

import (
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/automod/countstore"
)

// does not count "self-replies" (direct to self, or in own post thread)
func ReplyCountPostRule(evt *automod.RecordEvent, post *appbsky.FeedPost) error {
	if post.Reply == nil || IsSelfThread(evt, post) {
		return nil
	}

	did := evt.Account.Identity.DID.String()
	if evt.GetCount("reply", did, countstore.PeriodDay) > 3 {
		// TODO: disabled, too noisy for prod
		//evt.AddAccountFlag("frequent-replier")
	}
	evt.Increment("reply", did)

	parentURI, err := syntax.ParseATURI(post.Reply.Parent.Uri)
	if err != nil {
		evt.Logger.Warn("failed to parse reply AT-URI", "uri", post.Reply.Parent.Uri)
		return nil
	}
	evt.IncrementDistinct("reply-to", did, parentURI.Authority().String())
	return nil
}

var identicalReplyLimit = 5

// Looks for accounts posting the exact same text multiple times. Does not currently count the number of distinct accounts replied to, just counts replies at all.
//
// There can be legitimate situations that trigger this rule, so in most situations should be a "report" not "label" action.
func IdenticalReplyPostRule(evt *automod.RecordEvent, post *appbsky.FeedPost) error {
	if post.Reply == nil || IsSelfThread(evt, post) {
		return nil
	}

	// use a specific period (IncrementPeriod()) to reduce the number of counters (one per unique post text)
	period := automod.PeriodDay
	bucket := evt.Account.Identity.DID.String() + "/" + HashOfString(post.Text)
	if evt.GetCount("reply-text", bucket, period) >= identicalReplyLimit {
		evt.AddAccountFlag("multi-identical-reply")
	}

	evt.IncrementPeriod("reply-text", bucket, period)
	return nil
}
