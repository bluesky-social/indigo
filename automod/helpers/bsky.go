package helpers

import (
	"fmt"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/automod/keyword"
)

func ExtractHashtagsPost(post *appbsky.FeedPost) []string {
	var tags []string
	tags = append(tags, post.Tags...)
	for _, facet := range post.Facets {
		for _, feat := range facet.Features {
			if feat.RichtextFacet_Tag != nil {
				tags = append(tags, feat.RichtextFacet_Tag.Tag)
			}
		}
	}
	return DedupeStrings(tags)
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
	return DedupeStrings(out)
}

func ExtractBlobCIDsProfile(profile *appbsky.ActorProfile) []string {
	var out []string
	if profile.Avatar != nil {
		out = append(out, profile.Avatar.Ref.String())
	}
	if profile.Banner != nil {
		out = append(out, profile.Banner.Ref.String())
	}
	return DedupeStrings(out)
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

func ParentOrRootIsFollower(c *automod.RecordContext, post *appbsky.FeedPost) bool {
	if post.Reply == nil || IsSelfThread(c, post) {
		return false
	}

	parentURI, err := syntax.ParseATURI(post.Reply.Parent.Uri)
	if err != nil {
		c.Logger.Warn("failed to parse reply AT-URI", "uri", post.Reply.Parent.Uri)
		return false
	}
	parentDID, err := parentURI.Authority().AsDID()
	if err != nil {
		c.Logger.Warn("reply AT-URI authority not a DID", "uri", post.Reply.Parent.Uri)
		return false
	}

	rel := c.GetAccountRelationship(parentDID)
	if rel.FollowedBy {
		return true
	}

	rootURI, err := syntax.ParseATURI(post.Reply.Root.Uri)
	if err != nil {
		c.Logger.Warn("failed to parse reply AT-URI", "uri", post.Reply.Root.Uri)
		return false
	}
	rootDID, err := rootURI.Authority().AsDID()
	if err != nil {
		c.Logger.Warn("reply AT-URI authority not a DID", "uri", post.Reply.Root.Uri)
		return false
	}

	if rootDID == parentDID {
		return false
	}

	rel = c.GetAccountRelationship(rootDID)
	if rel.FollowedBy {
		return true
	}
	return false
}

func PostParentOrRootIsDid(post *appbsky.FeedPost, did string) bool {
	if post.Reply == nil {
		return false
	}

	rootUri, err := syntax.ParseATURI(post.Reply.Root.Uri)
	if err != nil || !rootUri.Authority().IsDID() {
		return false
	}

	parentUri, err := syntax.ParseATURI(post.Reply.Parent.Uri)
	if err != nil || !parentUri.Authority().IsDID() {
		return false
	}

	return rootUri.Authority().String() == did || parentUri.Authority().String() == did
}

func PostParentOrRootIsAnyDid(post *appbsky.FeedPost, dids []string) bool {
	if post.Reply == nil {
		return false
	}

	for _, did := range dids {
		if PostParentOrRootIsDid(post, did) {
			return true
		}
	}

	return false
}

func PostMentionsDid(post *appbsky.FeedPost, did string) bool {
	facets, err := ExtractFacets(post)
	if err != nil {
		return false
	}

	for _, facet := range facets {
		if facet.DID != nil && *facet.DID == did {
			return true
		}
	}

	return false
}

func PostMentionsAnyDid(post *appbsky.FeedPost, dids []string) bool {
	for _, did := range dids {
		if PostMentionsDid(post, did) {
			return true
		}
	}

	return false
}
