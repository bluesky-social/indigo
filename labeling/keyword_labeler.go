package labeling

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
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
	Keywords []string `json:"keywords"`
	Value    string   `json:"value"`
}

func (kl KeywordLabeler) LabelText(txt string) []string {
	txt = strings.ToLower(txt)
	for _, word := range kl.Keywords {
		if strings.Contains(txt, word) {
			return []string{kl.Value}
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

func LoadKeywordFile(fpath string) ([]KeywordLabeler, error) {

	var kwl []KeywordLabeler

	jsonFile, err := os.Open(fpath)
	if err != nil {
		return kwl, fmt.Errorf("failed to load JSON file: %v", err)
	}
	defer jsonFile.Close()

	raw, err := io.ReadAll(jsonFile)
	if err != nil {
		return kwl, fmt.Errorf("failed to load JSON file: %v", err)
	}

	if err = json.Unmarshal(raw, &kwl); err != nil {
		return kwl, fmt.Errorf("failed to parse Keyword file: %v", err)
	}

	return kwl, nil
}
