package rules

import (
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/automod"
)

func ReplyCountPostRule(evt *automod.RecordEvent, post *appbsky.FeedPost) error {
	if post.Reply != nil {
		did := evt.Account.Identity.DID.String()
		if evt.GetCount("reply", did, automod.PeriodDay) > 3 {
			// TODO: disabled, too noisy for prod
			//evt.AddAccountFlag("frequent-replier")
		}
		evt.Increment("reply", did)
	}
	return nil
}
