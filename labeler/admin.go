package labeler

import (
	"context"
	"fmt"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/models"
)

// This is probably only a temporary method
func (s *Server) hydrateRepoView(ctx context.Context, did, indexedAt string) *comatproto.AdminDefs_RepoView {
	return &comatproto.AdminDefs_RepoView{
		// TODO(bnewbold): populate more, or more correctly, from some backend?
		Did:            did,
		Email:          nil,
		Handle:         "TODO",
		IndexedAt:      indexedAt,
		Moderation:     nil,
		RelatedRecords: nil,
	}
}

// This is probably only a temporary method
func (s *Server) hydrateRecordView(ctx context.Context, did string, uri, cid *string, indexedAt string) *comatproto.AdminDefs_RecordView {
	repoView := s.hydrateRepoView(ctx, did, indexedAt)
	// TODO(bnewbold): populate more, or more correctly, from some backend?
	recordView := comatproto.AdminDefs_RecordView{
		BlobCids:   []string{},
		IndexedAt:  indexedAt,
		Moderation: nil,
		Repo:       repoView,
		// TODO: replace with actual record (from proxied backend)
		Value: &lexutil.LexiconTypeDecoder{Val: &appbsky.FeedPost{}},
	}
	if uri != nil {
		recordView.Uri = *uri
	}
	if cid != nil {
		recordView.Cid = *cid
	}
	return &recordView
}

func (s *Server) hydrateModerationReportViews(ctx context.Context, rows []models.ModerationReport) ([]*comatproto.AdminDefs_ReportView, error) {

	var out []*comatproto.AdminDefs_ReportView
	for _, row := range rows {
		var resolvedByActionIds []int64
		var actionRows []models.ModerationAction
		result := s.db.Joins("left join moderation_report_resolutions on moderation_report_resolutions.action_id = moderation_actions.id").Where("moderation_report_resolutions.report_id = ?", row.ID).Where("moderation_actions.reversed_at IS NULL").Find(&actionRows)
		if result.Error != nil {
			return nil, result.Error
		}
		for _, actionRow := range actionRows {
			resolvedByActionIds = append(resolvedByActionIds, int64(actionRow.ID))
		}

		var subj *comatproto.AdminDefs_ReportView_Subject
		switch row.SubjectType {
		case "com.atproto.repo.repoRef":
			subj = &comatproto.AdminDefs_ReportView_Subject{
				AdminDefs_RepoRef: &comatproto.AdminDefs_RepoRef{
					LexiconTypeID: "com.atproto.repo.repoRef",
					Did:           row.SubjectDid,
				},
			}
		case "com.atproto.repo.recordRef":
			subj = &comatproto.AdminDefs_ReportView_Subject{
				RepoStrongRef: &comatproto.RepoStrongRef{
					LexiconTypeID: "com.atproto.repo.strongRef",
					Uri:           *row.SubjectUri,
					Cid:           *row.SubjectCid,
				},
			}
		default:
			return nil, fmt.Errorf("unsupported moderation SubjectType: %v", row.SubjectType)
		}

		view := &comatproto.AdminDefs_ReportView{
			Id:                  int64(row.ID),
			Comment:             row.Reason,
			ReasonType:          &row.ReasonType,
			Subject:             subj,
			ReportedBy:          row.ReportedByDid,
			CreatedAt:           row.CreatedAt.Format(time.RFC3339),
			ResolvedByActionIds: resolvedByActionIds,
		}
		out = append(out, view)
	}
	return out, nil
}

func (s *Server) hydrateModerationReportDetails(ctx context.Context, rows []models.ModerationReport) ([]*comatproto.AdminDefs_ReportViewDetail, error) {

	var out []*comatproto.AdminDefs_ReportViewDetail
	for _, row := range rows {
		var actionRows []models.ModerationAction
		result := s.db.Joins("left join moderation_report_resolutions on moderation_report_resolutions.action_id = moderation_actions.id").Where("moderation_report_resolutions.report_id = ?", row.ID).Where("moderation_actions.reversed_at IS NULL").Find(&actionRows)
		if result.Error != nil {
			return nil, result.Error
		}

		var subj *comatproto.AdminDefs_ReportViewDetail_Subject
		switch row.SubjectType {
		case "com.atproto.repo.repoRef":
			subj = &comatproto.AdminDefs_ReportViewDetail_Subject{
				AdminDefs_RepoView: s.hydrateRepoView(ctx, row.SubjectDid, row.CreatedAt.Format(time.RFC3339)),
			}
		case "com.atproto.repo.recordRef":
			subj = &comatproto.AdminDefs_ReportViewDetail_Subject{
				AdminDefs_RecordView: s.hydrateRecordView(ctx, row.SubjectDid, row.SubjectUri, row.SubjectCid, row.CreatedAt.Format(time.RFC3339)),
			}
		default:
			return nil, fmt.Errorf("unsupported moderation SubjectType: %v", row.SubjectType)
		}

		viewDetail := &comatproto.AdminDefs_ReportViewDetail{
			Id:         int64(row.ID),
			Comment:    row.Reason,
			ReasonType: &row.ReasonType,
			Subject:    subj,
			ReportedBy: row.ReportedByDid,
			CreatedAt:  row.CreatedAt.Format(time.RFC3339),
		}
		out = append(out, viewDetail)
	}
	return out, nil
}
