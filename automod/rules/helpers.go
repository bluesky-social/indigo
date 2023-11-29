package rules

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
)

func dedupeStrings(in []string) []string {
	var out []string
	seen := make(map[string]bool)
	for _, v := range in {
		if !seen[v] {
			out = append(out, v)
			seen[v] = true
		}
	}
	return out
}

func ExtractHashtags(post *appbsky.FeedPost) []string {
	var tags []string
	for _, tag := range post.Tags {
		tags = append(tags, tag)
	}
	for _, facet := range post.Facets {
		for _, feat := range facet.Features {
			if feat.RichtextFacet_Tag != nil {
				tags = append(tags, feat.RichtextFacet_Tag.Tag)
			}
		}
	}
	return dedupeStrings(tags)
}

func NormalizeHashtag(raw string) string {
	return strings.ToLower(raw)
}

type PostFacet struct {
	Text string
	URL  *string
	DID  *string
	Tag  *string
}

func ExtractFacets(post *appbsky.FeedPost) ([]PostFacet, error) {
	var out []PostFacet

	for _, facet := range post.Facets {
		for _, feat := range facet.Features {
			if int(facet.Index.ByteEnd) > len([]byte(post.Text)) || facet.Index.ByteStart > facet.Index.ByteEnd {
				return nil, fmt.Errorf("invalid facet byte range")
			}

			txt := string([]byte(post.Text)[facet.Index.ByteStart:facet.Index.ByteEnd])
			if txt == "" {
				return nil, fmt.Errorf("empty facet text")
			}

			if feat.RichtextFacet_Link != nil {
				out = append(out, PostFacet{
					Text: txt,
					URL:  &feat.RichtextFacet_Link.Uri,
				})
			}
			if feat.RichtextFacet_Tag != nil {
				out = append(out, PostFacet{
					Text: txt,
					Tag:  &feat.RichtextFacet_Tag.Tag,
				})
			}
			if feat.RichtextFacet_Mention != nil {
				out = append(out, PostFacet{
					Text: txt,
					DID:  &feat.RichtextFacet_Mention.Did,
				})
			}
		}
	}
	return out, nil
}

func ExtractPostBlobCIDsPost(post *appbsky.FeedPost) []string {
	var out []string
	if post.Embed.EmbedImages != nil {
		for _, img := range post.Embed.EmbedImages.Images {
			out = append(out, img.Image.Ref.String())
		}
	}
	if post.Embed.EmbedRecordWithMedia != nil {
		media := post.Embed.EmbedRecordWithMedia.Media
		if media.EmbedImages != nil {
			for _, img := range media.EmbedImages.Images {
				out = append(out, img.Image.Ref.String())
			}
		}
	}
	return dedupeStrings(out)
}

func ExtractBlobCIDsProfile(profile *appbsky.ActorProfile) []string {
	var out []string
	if profile.Avatar != nil {
		out = append(out, profile.Avatar.Ref.String())
	}
	if profile.Banner != nil {
		out = append(out, profile.Banner.Ref.String())
	}
	return dedupeStrings(out)
}

// NOTE: this function has not been optimiszed at all!
func ExtractTextTokens(raw string) []string {
	raw = strings.ToLower(raw)
	f := func(c rune) bool {
		return !unicode.IsLetter(c) && !unicode.IsNumber(c)
	}
	return strings.FieldsFunc(raw, f)
}

func ExtractTextTokensPost(post *appbsky.FeedPost) []string {
	return ExtractTextTokens(post.Text)
}

func ExtractTextTokensProfile(profile *appbsky.ActorProfile) []string {
	s := ""
	if profile.Description != nil {
		s += " " + *profile.Description
	}
	if profile.DisplayName != nil {
		s += " " + *profile.DisplayName
	}
	return ExtractTextTokens(s)
}

// based on: https://stackoverflow.com/a/48769624, with no trailing period allowed
var urlRegex = regexp.MustCompile(`(?:(?:https?|ftp):\/\/)?[\w/\-?=%.]+\.[\w/\-&?=%.]*[\w/\-&?=%]+`)

func ExtractTextURLs(raw string) []string {
	return urlRegex.FindAllString(raw, -1)
}

func ExtractTextURLsProfile(profile *appbsky.ActorProfile) []string {
	s := ""
	if profile.Description != nil {
		s += " " + *profile.Description
	}
	if profile.DisplayName != nil {
		s += " " + *profile.DisplayName
	}
	return ExtractTextURLs(s)
}
