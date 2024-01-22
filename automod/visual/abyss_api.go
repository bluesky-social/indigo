package visual

import (
	lexutil "github.com/bluesky-social/indigo/lex/util"
)

type AbyssScanResp struct {
	Blob     *lexutil.LexBlob     `json:"blob"`
	Match    *AbyssMatchResult    `json:"match,omitempty"`
	Classify *AbyssClassifyResult `json:"classify,omitempty"`
	Review   *AbyssReviewState    `json:"review,omitempty"`
}

type AbyssMatchResult struct {
	Status string          `json:"status"`
	Hits   []AbyssMatchHit `json:"hits"`
}

type AbyssMatchHit struct {
	HashType  string `json:"hashType,omitempty"`
	HashValue string `json:"hashValue,omitempty"`
	Label     string `json:"label,omitempty"`
	// TODO: Corpus
}

type AbyssClassifyResult struct {
	// TODO
}

type AbyssReviewState struct {
	State    string `json:"state,omitempty"`
	TicketID string `json:"ticketId,omitempty"`
}

func (amr *AbyssMatchResult) IsAbuseMatch() bool {
	for _, hit := range amr.Hits {
		if hit.Label == "csam" || hit.Label == "csem" {
			return true
		}
	}
	return false
}
