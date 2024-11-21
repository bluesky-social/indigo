package search

import (
	"log/slog"
	"net/url"
	"strings"
	"time"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/rivo/uniseg"
)

type ProfileDoc struct {
	DocIndexTs  string   `json:"doc_index_ts"`
	DID         string   `json:"did"`
	RecordCID   string   `json:"record_cid"`
	Handle      string   `json:"handle"`
	DisplayName *string  `json:"display_name,omitempty"`
	Description *string  `json:"description,omitempty"`
	ImgAltText  []string `json:"img_alt_text,omitempty"`
	SelfLabel   []string `json:"self_label,omitempty"`
	URL         []string `json:"url,omitempty"`
	Domain      []string `json:"domain,omitempty"`
	Tag         []string `json:"tag,omitempty"`
	Emoji       []string `json:"emoji,omitempty"`
	HasAvatar   bool     `json:"has_avatar"`
	HasBanner   bool     `json:"has_banner"`
}

type PostDoc struct {
	DocIndexTs        string   `json:"doc_index_ts"`
	DID               string   `json:"did"`
	RecordRkey        string   `json:"record_rkey"`
	RecordCID         string   `json:"record_cid"`
	CreatedAt         *string  `json:"created_at,omitempty"`
	Text              string   `json:"text"`
	TextJA            *string  `json:"text_ja,omitempty"`
	LangCode          []string `json:"lang_code,omitempty"`
	LangCodeIso2      []string `json:"lang_code_iso2,omitempty"`
	MentionDID        []string `json:"mention_did,omitempty"`
	EmbedATURI        *string  `json:"embed_aturi,omitempty"`
	ReplyRootATURI    *string  `json:"reply_root_aturi,omitempty"`
	EmbedImgCount     int      `json:"embed_img_count"`
	EmbedImgAltText   []string `json:"embed_img_alt_text,omitempty"`
	EmbedImgAltTextJA []string `json:"embed_img_alt_text_ja,omitempty"`
	SelfLabel         []string `json:"self_label,omitempty"`
	URL               []string `json:"url,omitempty"`
	Domain            []string `json:"domain,omitempty"`
	Tag               []string `json:"tag,omitempty"`
	Emoji             []string `json:"emoji,omitempty"`
}

// Returns the search index document ID (`_id`) for this document.
//
// This identifier should be URL safe and not contain a slash ("/").
func (d *ProfileDoc) DocId() string {
	return d.DID
}

// Returns the search index document ID (`_id`) for this document.
//
// This identifier should be URL safe and not contain a slash ("/").
func (d *PostDoc) DocId() string {
	return d.DID + "_" + d.RecordRkey
}

func TransformProfile(profile *appbsky.ActorProfile, ident *identity.Identity, cid string) ProfileDoc {
	// TODO: placeholder for future alt text on profile blobs
	var altText []string
	var tags []string
	var emojis []string
	if profile.Description != nil {
		tags = parseProfileTags(profile)
		emojis = parseEmojis(*profile.Description)
	}
	var selfLabels []string
	if profile.Labels != nil && profile.Labels.LabelDefs_SelfLabels != nil {
		for _, le := range profile.Labels.LabelDefs_SelfLabels.Values {
			selfLabels = append(selfLabels, le.Val)
		}
	}
	handle := ""
	if !ident.Handle.IsInvalidHandle() {
		handle = ident.Handle.String()
	}
	return ProfileDoc{
		DocIndexTs:  syntax.DatetimeNow().String(),
		DID:         ident.DID.String(),
		RecordCID:   cid,
		Handle:      handle,
		DisplayName: profile.DisplayName,
		Description: profile.Description,
		ImgAltText:  altText,
		SelfLabel:   selfLabels,
		Tag:         tags,
		Emoji:       emojis,
		HasAvatar:   profile.Avatar != nil,
		HasBanner:   profile.Banner != nil,
	}
}

func TransformPost(post *appbsky.FeedPost, did syntax.DID, rkey, cid string) PostDoc {
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
	var mentionDIDs []string
	var urls []string
	for _, facet := range post.Facets {
		for _, feat := range facet.Features {
			if feat.RichtextFacet_Mention != nil {
				mentionDIDs = append(mentionDIDs, feat.RichtextFacet_Mention.Did)
			}
			if feat.RichtextFacet_Link != nil {
				urls = append(urls, feat.RichtextFacet_Link.Uri)
			}
		}
	}
	var replyRootATURI *string
	if post.Reply != nil {
		replyRootATURI = &(post.Reply.Root.Uri)
	}
	if post.Embed != nil && post.Embed.EmbedExternal != nil {
		urls = append(urls, post.Embed.EmbedExternal.External.Uri)
	}
	var embedATURI *string
	if post.Embed != nil && post.Embed.EmbedRecord != nil {
		embedATURI = &post.Embed.EmbedRecord.Record.Uri
	}
	if post.Embed != nil && post.Embed.EmbedRecordWithMedia != nil {
		embedATURI = &post.Embed.EmbedRecordWithMedia.Record.Record.Uri
	}
	var embedImgCount int
	var embedImgAltText []string
	var embedImgAltTextJA []string
	if post.Embed != nil && post.Embed.EmbedImages != nil {
		embedImgCount = len(post.Embed.EmbedImages.Images)
		for _, img := range post.Embed.EmbedImages.Images {
			if img.Alt != "" {
				embedImgAltText = append(embedImgAltText, img.Alt)
				if containsJapanese(img.Alt) {
					embedImgAltTextJA = append(embedImgAltTextJA, img.Alt)
				}
			}
		}
	}

	if post.Embed != nil &&
		post.Embed.EmbedRecordWithMedia != nil &&
		post.Embed.EmbedRecordWithMedia.Media != nil &&
		post.Embed.EmbedRecordWithMedia.Media.EmbedImages != nil &&
		len(post.Embed.EmbedRecordWithMedia.Media.EmbedImages.Images) > 0 {
		embedImgCount += len(post.Embed.EmbedRecordWithMedia.Media.EmbedImages.Images)
		for _, img := range post.Embed.EmbedRecordWithMedia.Media.EmbedImages.Images {
			if img.Alt != "" {
				embedImgAltText = append(embedImgAltText, img.Alt)
				if containsJapanese(img.Alt) {
					embedImgAltTextJA = append(embedImgAltTextJA, img.Alt)
				}
			}
		}
	}

	var selfLabels []string
	if post.Labels != nil && post.Labels.LabelDefs_SelfLabels != nil {
		for _, le := range post.Labels.LabelDefs_SelfLabels.Values {
			selfLabels = append(selfLabels, le.Val)
		}
	}

	var domains []string
	for i, raw := range urls {
		clean := NormalizeLossyURL(raw)
		urls[i] = clean
		u, err := url.Parse(clean)
		if nil == err {
			domains = append(domains, u.Hostname())
		}
	}

	doc := PostDoc{
		DocIndexTs:        syntax.DatetimeNow().String(),
		DID:               did.String(),
		RecordRkey:        rkey,
		RecordCID:         cid,
		Text:              post.Text,
		LangCode:          post.Langs,
		LangCodeIso2:      langCodeIso2,
		MentionDID:        mentionDIDs,
		EmbedATURI:        embedATURI,
		ReplyRootATURI:    replyRootATURI,
		EmbedImgCount:     embedImgCount,
		EmbedImgAltText:   embedImgAltText,
		EmbedImgAltTextJA: embedImgAltTextJA,
		SelfLabel:         selfLabels,
		URL:               urls,
		Domain:            domains,
		Tag:               parsePostTags(post),
		Emoji:             parseEmojis(post.Text),
	}

	if containsJapanese(post.Text) {
		doc.TextJA = &post.Text
	}

	if post.CreatedAt != "" {
		// there are some old bad timestamps out there!
		dt, err := syntax.ParseDatetimeLenient(post.CreatedAt)
		if nil == err { // *not* an error
			// not more than a few minutes in the future
			if time.Since(dt.Time()) >= -1*5*time.Minute {
				s := dt.String()
				doc.CreatedAt = &s
			} else {
				slog.Warn("rejecting future post CreatedAt", "datetime", dt.String(), "did", did.String(), "rkey", rkey)
				s := syntax.DatetimeNow().String()
				doc.CreatedAt = &s
			}
		}
	}

	return doc
}

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

func parseProfileTags(p *appbsky.ActorProfile) []string {
	// TODO: waiting for profile tag lexicon support
	var ret []string = []string{}
	if len(ret) == 0 {
		return nil
	}
	return dedupeStrings(ret)
}

func parsePostTags(p *appbsky.FeedPost) []string {
	var ret []string = []string{}
	for _, facet := range p.Facets {
		for _, feat := range facet.Features {
			if feat.RichtextFacet_Tag != nil {
				ret = append(ret, feat.RichtextFacet_Tag.Tag)
			}
		}
	}
	ret = append(ret, p.Tags...)
	if len(ret) == 0 {
		return nil
	}
	return dedupeStrings(ret)
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
