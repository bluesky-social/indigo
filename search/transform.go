package search

import (
	"regexp"
	"strings"
	"time"

	bsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/util"
	"github.com/rivo/uniseg"
)

type ProfileDoc struct {
	DocIndexTs  string   `json:"doc_index_ts"`
	Did         string   `json:"did"`
	RecordCid   string   `json:"record_cid"`
	Handle      string   `json:"handle"`
	DisplayName *string  `json:"display_name,omitempty"`
	Description *string  `json:"description,omitempty"`
	ImgAltText  []string `json:"img_alt_text,omitempty"`
	Hashtag     []string `json:"hashtag,omitempty"`
	Emoji       []string `json:"emoji,omitempty"`
	HasAvatar   bool     `json:"has_avatar"`
	HasBanner   bool     `json:"has_banner"`
}

type PostDoc struct {
	DocIndexTs      string   `json:"doc_index_ts"`
	Did             string   `json:"did"`
	RecordRkey      string   `json:"record_rkey"`
	RecordCid       string   `json:"record_cid"`
	Handle          string   `json:"handle"`
	CreatedAt       string   `json:"created_at"`
	Text            string   `json:"text"`
	LangCode        []string `json:"lang_code,omitempty"`
	LangCodeIso2    []string `json:"lang_code_iso2,omitempty"`
	MentionDid      []string `json:"mention_did,omitempty"`
	LinkUrl         []string `json:"link_url,omitempty"`
	EmbedUrl        *string  `json:"embed_url,omitempty"`
	EmbedAturi      *string  `json:"embed_aturi,omitempty"`
	ReplyRootAturi  *string  `json:"reply_root_aturi,omitempty"`
	EmbedImgCount   int      `json:"embed_img_count"`
	EmbedImgAltText []string `json:"embed_img_alt_text,omitempty"`
	Hashtag         []string `json:"hashtag,omitempty"`
	Emoji           []string `json:"emoji,omitempty"`
}

// Returns the search index document ID (`_id`) for this document.
//
// This identifier should be URL safe and not contain a slash ("/").
func (d *ProfileDoc) DocId() string {
	return d.Did
}

// Returns the search index document ID (`_id`) for this document.
//
// This identifier should be URL safe and not contain a slash ("/").
func (d *PostDoc) DocId() string {
	return d.Did + "_" + d.RecordRkey
}

func TransformProfile(profile *bsky.ActorProfile, repo *User, cid string) ProfileDoc {
	// TODO: placeholder for future alt text on profile blobs
	var altText []string
	var hashtags []string
	var emojis []string
	if profile.Description != nil {
		hashtags = parseHashtags(*profile.Description)
		emojis = parseEmojis(*profile.Description)
	}
	return ProfileDoc{
		DocIndexTs:  time.Now().UTC().Format(util.ISO8601),
		Did:         repo.Did,
		RecordCid:   cid,
		Handle:      repo.Handle,
		DisplayName: profile.DisplayName,
		Description: profile.Description,
		ImgAltText:  altText,
		Hashtag:     hashtags,
		Emoji:       emojis,
		HasAvatar:   profile.Avatar != nil,
		HasBanner:   profile.Banner != nil,
	}
}

func TransformPost(post *bsky.FeedPost, repo *User, rkey, cid string) PostDoc {
	altText := []string{}
	if post.Embed != nil && post.Embed.EmbedImages != nil {
		for _, img := range post.Embed.EmbedImages.Images {
			if img.Alt != "" {
				altText = append(altText, img.Alt)
			}
		}
	}
	var langCodeIso2 []string
	for _, lang := range post.Langs {
		// TODO: include an actual language code map to go from 3char to 2char
		prefix := strings.SplitN(lang, "-", 2)[0]
		if len(prefix) == 2 {
			langCodeIso2 = append(langCodeIso2, strings.ToLower(prefix))
		}
	}
	var mentionDids []string
	var linkUrls []string
	for _, facet := range post.Facets {
		for _, feat := range facet.Features {
			if feat.RichtextFacet_Mention != nil {
				mentionDids = append(mentionDids, feat.RichtextFacet_Mention.Did)
			}
			if feat.RichtextFacet_Link != nil {
				linkUrls = append(linkUrls, feat.RichtextFacet_Link.Uri)
			}
		}
	}
	var replyRootAturi *string
	if post.Reply != nil {
		replyRootAturi = &(post.Reply.Root.Uri)
	}
	var embedUrl *string
	if post.Embed != nil && post.Embed.EmbedExternal != nil {
		embedUrl = &post.Embed.EmbedExternal.External.Uri
	}
	var embedAturi *string
	if post.Embed != nil && post.Embed.EmbedRecord != nil {
		embedAturi = &post.Embed.EmbedRecord.Record.Uri
	}
	if post.Embed != nil && post.Embed.EmbedRecordWithMedia != nil {
		embedAturi = &post.Embed.EmbedRecordWithMedia.Record.Record.Uri
	}
	var embedImgCount int = 0
	var embedImgAltText []string
	if post.Embed != nil && post.Embed.EmbedImages != nil {
		embedImgCount = len(post.Embed.EmbedImages.Images)
		for _, img := range post.Embed.EmbedImages.Images {
			if img.Alt != "" {
				embedImgAltText = append(embedImgAltText, img.Alt)
			}
		}
	}
	return PostDoc{
		DocIndexTs:      time.Now().UTC().Format(util.ISO8601),
		Did:             repo.Did,
		RecordRkey:      rkey,
		RecordCid:       cid,
		Handle:          repo.Handle,
		CreatedAt:       post.CreatedAt,
		Text:            post.Text,
		LangCode:        post.Langs,
		LangCodeIso2:    langCodeIso2,
		MentionDid:      mentionDids,
		LinkUrl:         linkUrls,
		EmbedUrl:        embedUrl,
		EmbedAturi:      embedAturi,
		ReplyRootAturi:  replyRootAturi,
		EmbedImgCount:   embedImgCount,
		EmbedImgAltText: embedImgAltText,
		Hashtag:         parseHashtags(post.Text),
		Emoji:           parseEmojis(post.Text),
	}
}

func parseHashtags(s string) []string {
	var hashtagRegex = regexp.MustCompile(`\B#([A-Za-z]+)\b`)
	var ret []string = []string{}
	seen := make(map[string]bool)
	for _, m := range hashtagRegex.FindAllStringSubmatch(s, -1) {
		if seen[m[1]] == false {
			ret = append(ret, m[1])
			seen[m[1]] = true
		}
	}
	if len(ret) == 0 {
		return nil
	}
	return ret
}

func parseEmojis(s string) []string {
	var ret []string = []string{}
	seen := make(map[string]bool)
	gr := uniseg.NewGraphemes(s)
	for gr.Next() {
		// check if this grapheme cluster starts with an emoji rune (Unicode codepoint, int32)
		firstRune := gr.Runes()[0]
		if (firstRune >= 0x1F000 && firstRune <= 0x1FFFF) || (firstRune >= 0x2600 && firstRune <= 0x26FF) {
			emoji := gr.Str()
			if seen[emoji] == false {
				ret = append(ret, emoji)
				seen[emoji] = true
			}
		}
	}
	if len(ret) == 0 {
		return nil
	}
	return ret
}
