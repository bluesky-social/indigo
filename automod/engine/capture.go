package engine

import (
	"context"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod/event"
)

// REVIEW: if this "capture" code can leave the engine package.  It seems likely.

type AccountCapture struct {
	CapturedAt  syntax.Datetime                     `json:"capturedAt"`
	AccountMeta event.AccountMeta                   `json:"accountMeta"`
	PostRecords []comatproto.RepoListRecords_Record `json:"postRecords"`
}

func (e *Engine) CaptureRecent(ctx context.Context, atid syntax.AtIdentifier, limit int) (*AccountCapture, error) {
	ident, records, err := e.FetchRecent(ctx, atid, limit)
	if err != nil {
		return nil, err
	}
	pr := []comatproto.RepoListRecords_Record{}
	for _, r := range records {
		if r != nil {
			pr = append(pr, *r)
		}
	}

	// clear any pre-parsed key, which would fail to marshal as JSON
	ident.ParsedPublicKey = nil
	am, err := e.GetAccountMeta(ctx, ident)
	if err != nil {
		return nil, err
	}

	// auto-clear sensitive PII (eg, account email)
	am.Private = nil

	ac := AccountCapture{
		CapturedAt:  syntax.DatetimeNow(),
		AccountMeta: *am,
		PostRecords: pr,
	}
	return &ac, nil
}
