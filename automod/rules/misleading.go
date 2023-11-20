package rules

import (
	"context"
	"net/url"
	"strings"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod"
)

func MisleadingURLPostRule(evt *automod.RecordEvent, post *appbsky.FeedPost) error {
	facets, err := ExtractFacets(post)
	if err != nil {
		evt.Logger.Warn("invalid facets", "err", err)
		evt.AddRecordFlag("invalid") // TODO: or some other "this record is corrupt" indicator?
		return nil
	}
	for _, facet := range facets {
		if facet.URL != nil {
			linkURL, err := url.Parse(*facet.URL)
			if err != nil {
				evt.Logger.Warn("invalid link metadata URL", "url", facet.URL)
				continue
			}

			// basic text string pre-cleanups
			text := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(facet.Text), "..."))
			// if really not a domain, just skipp
			if !strings.Contains(text, ".") {
				continue
			}
			// try to fix any missing method in the text
			if !strings.Contains(text, "://") {
				text = "https://" + text
			}

			// try parsing as a full URL (with whitespace trimmed)
			textURL, err := url.Parse(text)
			if err != nil {
				evt.Logger.Warn("invalid link text URL", "url", facet.Text)
				continue
			}

			// for now just compare domains to handle the most obvious cases
			// this public code will obviously get discovered and bypassed. this doesn't earn you any security cred!
			if linkURL.Host != textURL.Host && linkURL.Host != "www."+linkURL.Host {
				evt.Logger.Warn("misleading mismatched domains", "linkHost", linkURL.Host, "textHost", textURL.Host, "text", facet.Text)
				evt.AddRecordFlag("misleading")
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
		evt.AddRecordFlag("invalid") // TODO: or some other "this record is corrupt" indicator?
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
				evt.AddRecordFlag("misleading")
				break
			}

			// TODO: check if mentioned DID was recently updated? might be a caching issue
			if mentioned.DID.String() != *facet.DID {
				evt.Logger.Warn("misleading mention", "text", txt, "did", facet.DID)
				evt.AddRecordFlag("misleading")
				continue
			}
		}
	}
	return nil
}
