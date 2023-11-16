package rules

import (
	"github.com/bluesky-social/indigo/automod"
)

func ReplyCountPostRule(evt *automod.PostEvent) error {
	if evt.Post.Reply != nil {
		did := evt.Account.Identity.DID.String()
		if evt.GetCount("reply", did, automod.PeriodDay) > 3 {
			evt.AddAccountFlag("frequent-replier")
		}
		evt.Increment("reply", did)
	}
	return nil
}
