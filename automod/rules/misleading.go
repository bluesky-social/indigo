package rules

import (
	"context"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod"
)

func MisleadingURLPostRule(evt *automod.PostEvent) error {
	for _, facet := range evt.Post.Facets {
		for _, feat := range facet.Features {
			if feat.RichtextFacet_Link != nil {
				if int(facet.Index.ByteEnd) > len([]byte(evt.Post.Text)) || facet.Index.ByteStart > facet.Index.ByteEnd {
					evt.Logger.Warn("invalid facet range")
					evt.AddLabel("invalid") // TODO: or some other "this record is corrupt" indicator?
					continue
				}
				txt := string([]byte(evt.Post.Text)[facet.Index.ByteStart:facet.Index.ByteEnd])

				linkURL, err := url.Parse(feat.RichtextFacet_Link.Uri)
				if err != nil {
					evt.Logger.Warn("invalid link metadata URL", "uri", feat.RichtextFacet_Link.Uri)
					continue
				}

				// try parsing as a full URL
				textURL, err := url.Parse(txt)
				if err != nil {
					evt.Logger.Warn("invalid link text URL", "uri", txt)
					continue
				}

				// for now just compare domains to handle the most obvious cases
				// this public code will obviously get discovered and bypassed. this doesn't earn you any security cred!
				if linkURL.Host != textURL.Host {
					evt.Logger.Warn("misleading mismatched domains", "link", linkURL.Host, "text", textURL.Host)
					evt.AddLabel("misleading")
				}
			}
		}
	}
	return nil
}

func MisleadingMentionPostRule(evt *automod.PostEvent) error {
	// TODO: do we really need to route context around? probably
	ctx := context.TODO()
	for _, facet := range evt.Post.Facets {
		for _, feat := range facet.Features {
			if feat.RichtextFacet_Mention != nil {
				if int(facet.Index.ByteEnd) > len([]byte(evt.Post.Text)) || facet.Index.ByteStart > facet.Index.ByteEnd {
					evt.Logger.Warn("invalid facet range")
					evt.AddLabel("invalid") // TODO: or some other "this record is corrupt" indicator?
					continue
				}
				txt := string([]byte(evt.Post.Text)[facet.Index.ByteStart:facet.Index.ByteEnd])
				if txt[0] == '@' {
					txt = txt[1:]
				}
				handle, err := syntax.ParseHandle(txt)
				if err != nil {
					evt.Logger.Warn("mention was not a valid handle", "text", txt)
					continue
				}

				mentioned, err := evt.Engine.Directory.LookupHandle(ctx, handle)
				if err != nil {
					evt.Logger.Warn("could not resolve handle", "handle", handle)
					evt.AddLabel("misleading")
					break
				}

				// TODO: check if mentioned DID was recently updated? might be a caching issue
				if mentioned.DID.String() != feat.RichtextFacet_Mention.Did {
					evt.Logger.Warn("misleading mention", "text", txt, "did", mentioned.DID)
					evt.AddLabel("misleading")
					continue
				}
			}
		}
	}
	return nil
}
