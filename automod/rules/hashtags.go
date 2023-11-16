package rules

import (
	"github.com/bluesky-social/indigo/automod"
)

func BanHashtagsPostRule(evt *automod.PostEvent) error {
	for _, tag := range evt.Post.Tags {
		if evt.InSet("banned-hashtags", tag) {
			evt.AddRecordLabel("bad-hashtag")
			break
		}
	}
	for _, facet := range evt.Post.Facets {
		for _, feat := range facet.Features {
			if feat.RichtextFacet_Tag != nil {
				tag := feat.RichtextFacet_Tag.Tag
				if evt.InSet("banned-hashtags", tag) {
					evt.AddRecordLabel("bad-hashtag")
					break
				}
			}
		}
	}
	return nil
}
