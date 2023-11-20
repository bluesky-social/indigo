package rules

import (
	"context"
	"net/url"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod"
)

func MisleadingURLPostRule(evt *automod.RecordEvent, post *appbsky.FeedPost) error {
	facets, err := ExtractFacets(post)
	if err != nil {
		evt.Logger.Warn("invalid facets", "err", err)
		evt.AddRecordLabel("invalid") // TODO: or some other "this record is corrupt" indicator?
		return nil
	}
	for _, facet := range facets {
		if facet.URL != nil {
			linkURL, err := url.Parse(*facet.URL)
			if err != nil {
				evt.Logger.Warn("invalid link metadata URL", "uri", facet.URL)
				continue
			}

			// try parsing as a full URL
			textURL, err := url.Parse(facet.Text)
			if err != nil {
				evt.Logger.Warn("invalid link text URL", "uri", facet.Text)
				continue
			}

			// for now just compare domains to handle the most obvious cases
			// this public code will obviously get discovered and bypassed. this doesn't earn you any security cred!
			if linkURL.Host != textURL.Host {
				evt.Logger.Warn("misleading mismatched domains", "link", linkURL.Host, "text", textURL.Host)
				evt.AddRecordLabel("misleading")
			}
		}
	}
	return nil
}

func MisleadingMentionPostRule(evt *automod.RecordEvent, post *appbsky.FeedPost) error {
	// TODO: do we really need to route context around? probably
	ctx := context.TODO()
	facets, err := ExtractFacets(post)
	if err != nil {
		evt.Logger.Warn("invalid facets", "err", err)
		evt.AddRecordLabel("invalid") // TODO: or some other "this record is corrupt" indicator?
		return nil
	}
	for _, facet := range facets {
		if facet.DID != nil {
			txt := facet.Text
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
				evt.AddRecordLabel("misleading")
				break
			}

			// TODO: check if mentioned DID was recently updated? might be a caching issue
			if mentioned.DID.String() != *facet.DID {
				evt.Logger.Warn("misleading mention", "text", txt, "did", facet.DID)
				evt.AddRecordLabel("misleading")
				continue
			}
		}
	}
	return nil
}
