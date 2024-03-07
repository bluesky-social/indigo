package capture

import (
	"context"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod"
)

type AccountCapture struct {
	CapturedAt  syntax.Datetime                     `json:"capturedAt"`
	AccountMeta automod.AccountMeta                 `json:"accountMeta"`
	PostRecords []comatproto.RepoListRecords_Record `json:"postRecords"`
}

func CaptureRecent(ctx context.Context, eng *automod.Engine, atid syntax.AtIdentifier, limit int) (*AccountCapture, error) {
	ident, records, err := FetchRecent(ctx, eng, atid, limit)
	if err != nil {
		return nil, err
	}
	pr := []comatproto.RepoListRecords_Record{}
	for _, r := range records {
		if r != nil {
			pr = append(pr, *r)
		}
	}

	am, err := eng.GetAccountMeta(ctx, ident)
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
