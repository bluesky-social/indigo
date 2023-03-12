package labeling

import (
	"strings"

	bsky "github.com/bluesky-social/indigo/api/bsky"
)

// simple record labeling (without pre-fetched blobs)
type SimplePostLabeler interface {
	LabelPost(p bsky.FeedPost) []string
}
type SimpleActorProfileLabeler interface {
	LabelActorProfile(ap bsky.ActorProfile) []string
}

type KeywordLabeler struct {
	keywords []string
	value    string
}

func (kl KeywordLabeler) LabelText(txt string) []string {
	txt = strings.ToLower(txt)
	for _, word := range kl.keywords {
		if strings.Contains(txt, word) {
			return []string{kl.value}
		}
	}
	return []string{}
}

func (kl KeywordLabeler) LabelPost(p bsky.FeedPost) []string {
	return kl.LabelText(p.Text)
}

func (kl KeywordLabeler) LabelActorProfile(ap bsky.ActorProfile) []string {
	txt := ap.DisplayName
	if ap.Description != nil {
		txt += *ap.Description
	}
	return kl.LabelText(txt)
}
