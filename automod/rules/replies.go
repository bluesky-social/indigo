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
