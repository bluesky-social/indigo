package rules

import (
	"fmt"
	"regexp"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/automod/keyword"

	"github.com/spaolacci/murmur3"
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

func ExtractHashtagsPost(post *appbsky.FeedPost) []string {
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
	return keyword.Slugify(raw)
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

func ExtractTextTokensPost(post *appbsky.FeedPost) []string {
	s := post.Text
	if post.Embed != nil {
		if post.Embed.EmbedImages != nil {
			for _, img := range post.Embed.EmbedImages.Images {
				if img.Alt != "" {
					s += " " + img.Alt
				}
			}
		}
		if post.Embed.EmbedRecordWithMedia != nil {
			media := post.Embed.EmbedRecordWithMedia.Media
			if media.EmbedImages != nil {
				for _, img := range media.EmbedImages.Images {
					if img.Alt != "" {
						s += " " + img.Alt
					}
				}
			}
		}
	}
	return keyword.TokenizeText(s)
}

func ExtractTextTokensProfile(profile *appbsky.ActorProfile) []string {
	s := ""
	if profile.Description != nil {
		s += " " + *profile.Description
	}
	if profile.DisplayName != nil {
		s += " " + *profile.DisplayName
	}
	return keyword.TokenizeText(s)
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

// checks if the post event is a reply post for which the author is replying to themselves, or author is the root author (OP)
func IsSelfThread(c *automod.RecordContext, post *appbsky.FeedPost) bool {
	if post.Reply == nil {
		return false
	}
	did := c.Account.Identity.DID.String()
	parentURI, err := syntax.ParseATURI(post.Reply.Parent.Uri)
	if err != nil {
		return false
	}
	rootURI, err := syntax.ParseATURI(post.Reply.Root.Uri)
	if err != nil {
		return false
	}

	if parentURI.Authority().String() == did || rootURI.Authority().String() == did {
		return true
	}
	return false
}

// returns a fast, compact hash of a string
//
// current implementation uses murmur3, default seed, and hex encoding
func HashOfString(s string) string {
	val := murmur3.Sum64([]byte(s))
	return fmt.Sprintf("%016x", val)
}
