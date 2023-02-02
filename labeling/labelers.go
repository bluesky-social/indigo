package labeling

import (
	"strings"

	bsky "github.com/bluesky-social/indigo/api/bsky"
)

// simple record labeling (without pre-fetched blobs)
type SimplePostLabeler interface {
	labelPost(p bsky.FeedPost) []string
}
type SimpleActorProfileLabeler interface {
	labelActorProfile(ap bsky.ActorProfile) []string
}

type KeywordLabeler struct {
	keywords []string
	value    string
}

func (kl KeywordLabeler) labelText(txt string) []string {
	txt = strings.ToLower(txt)
	for _, word := range kl.keywords {
		if strings.Contains(txt, word) {
			return []string{kl.value}
		}
	}
	return []string{}
}

func (kl KeywordLabeler) labelPost(p bsky.FeedPost) []string {
	return kl.labelText(p.Text)
}

func (kl KeywordLabeler) labelActorProfile(ap bsky.ActorProfile) []string {
	txt := ap.DisplayName
	if ap.Description != nil {
		txt += *ap.Description
	}
	return kl.labelText(txt)
}
