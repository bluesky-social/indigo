package rules

import (
	"log/slog"
	"net/url"
	"strings"
	"unicode"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/automod/helpers"
)

func isMisleadingURLFacet(facet helpers.PostFacet, logger *slog.Logger) bool {
	linkURL, err := url.Parse(*facet.URL)
	if err != nil {
		logger.Warn("invalid link metadata URL", "url", facet.URL)
		return false
	}

	// basic text string pre-cleanups
	text := strings.ToLower(strings.TrimSpace(facet.Text))

	// remove square brackets
	if strings.HasPrefix(text, "[") && strings.HasSuffix(text, "]") {
		text = text[1 : len(text)-1]
	}

	// truncated and not an obvious prefix hack (TODO: more special domains? regex?)
	if strings.HasSuffix(text, "...") && !strings.HasSuffix(text, ".com...") && !strings.HasSuffix(text, ".org...") {
		return false
	}
	if strings.HasSuffix(text, "…") && !strings.HasSuffix(text, ".com…") && !strings.HasSuffix(text, ".org…") {
		return false
	}

	// remove any other truncation suffix
	text = strings.TrimSuffix(strings.TrimSuffix(text, "..."), "…")

	if len(text) == 0 {
		logger.Warn("empty facet text", "text", facet.Text)
		return false
	}

	// if really not-a-domain, just skip
	if !strings.Contains(text, ".") {
		return false
	}

	// hostnames can't start with a digit (eg, arxiv or DOI links)
	for _, c := range text[0:1] {
		if unicode.IsNumber(c) {
			return false
		}
	}

	// try to fix any missing method in the text
	if !strings.Contains(text, "://") {
		text = "https://" + text
	}

	// try parsing as a full URL (with whitespace trimmed)
	textURL, err := url.Parse(text)
	if err != nil {
		logger.Warn("invalid link text URL", "url", facet.Text)
		return false
	}

	// for now just compare domains to handle the most obvious cases
	// this public code will obviously get discovered and bypassed. this doesn't earn you any security cred!
	linkHost := strings.TrimPrefix(strings.ToLower(linkURL.Host), "www.")
	textHost := strings.TrimPrefix(strings.ToLower(textURL.Host), "www.")
	if textHost != linkHost {
		logger.Warn("misleading mismatched domains", "linkHost", linkURL.Host, "textHost", textURL.Host, "text", facet.Text)
		return true
	}
	return false
}

var _ automod.PostRuleFunc = MisleadingURLPostRule

func MisleadingURLPostRule(c *automod.RecordContext, post *appbsky.FeedPost) error {
	// TODO: make this an InSet() config?
	if c.Account.Identity.Handle == "nowbreezing.ntw.app" {
		return nil
	}
	facets, err := helpers.ExtractFacets(post)
	if err != nil {
		c.Logger.Warn("invalid facets", "err", err)
		// TODO: or some other "this record is corrupt" indicator?
		//c.AddRecordFlag("broken-post")
		return nil
	}
	for _, facet := range facets {
		if facet.URL != nil {
			if isMisleadingURLFacet(facet, c.Logger) {
				c.AddRecordFlag("misleading-link")
				c.Notify("slack")
			}
		}
	}
	return nil
}

var _ automod.PostRuleFunc = MisleadingMentionPostRule

func MisleadingMentionPostRule(c *automod.RecordContext, post *appbsky.FeedPost) error {
	facets, err := helpers.ExtractFacets(post)
	if err != nil {
		c.Logger.Warn("invalid facets", "err", err)
		// TODO: or some other "this record is corrupt" indicator?
		//c.AddRecordFlag("broken-post")
		return nil
	}
	for _, facet := range facets {
		if facet.DID != nil {
			txt := facet.Text
			if txt[0] == '@' {
				txt = txt[1:]
			}
			handle, err := syntax.ParseHandle(strings.ToLower(txt))
			if err != nil {
				c.Logger.Warn("mention was not a valid handle", "text", txt)
				continue
			}

			mentioned, err := c.Directory().LookupHandle(c.Ctx, handle)
			if err != nil {
				c.Logger.Warn("could not resolve handle", "handle", handle)
				c.AddRecordFlag("broken-mention")
				c.Notify("slack")
				break
			}

			// TODO: check if mentioned DID was recently updated? might be a caching issue
			if mentioned.DID.String() != *facet.DID {
				c.Logger.Warn("misleading mention", "text", txt, "did", facet.DID)
				c.AddRecordFlag("misleading-mention")
				c.Notify("slack")
				continue
			}
		}
	}
	return nil
}
