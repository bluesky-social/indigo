package engine

import (
	"context"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// recordModState is the subset of a tools.ozone.moderation.getRecord response that persistRecordModActions needs to
// de-duplicate labels, tags, and review-state actions.
//
// It is decoded locally rather than with toolsozone.ModerationDefs_RecordViewDetail because that type strictly decodes
// the record `value` (and `repo.relatedRecords`) with lexutil.LexiconTypeDecoder, which returns ErrUnrecognizedType for
// any record type not compiled into this binary. Blob rules run on every collection, so decoding an unregistered
// record type can cause labels to be re-applied on updates.
type recordModState struct {
	Labels     []*comatproto.LabelDefs_Label `json:"labels,omitempty"`
	Moderation *recordModDetail              `json:"moderation,omitempty"`
}

type recordModDetail struct {
	SubjectStatus *recordSubjectStatus `json:"subjectStatus,omitempty"`
}

type recordSubjectStatus struct {
	ReviewState *string  `json:"reviewState,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

func fetchRecordModState(ctx context.Context, c *atclient.APIClient, uri syntax.ATURI, cid syntax.CID) (*recordModState, error) {
	var out recordModState
	params := map[string]any{
		"uri": uri.String(),
		"cid": cid.String(),
	}
	if err := c.Get(ctx, syntax.NSID("tools.ozone.moderation.getRecord"), params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
