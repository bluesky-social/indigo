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
		Value: &lexutil.LexiconTypeDecoder{&appbsky.FeedPost{}},
	}
	if uri != nil {
		recordView.Uri = *uri
	}
	if cid != nil {
		recordView.Cid = *cid
	}
	return &recordView
}

func (s *Server) hydrateModerationActionViews(ctx context.Context, rows []models.ModerationAction) ([]*comatproto.AdminDefs_ActionView, error) {

	var out []*comatproto.AdminDefs_ActionView

	for _, row := range rows {

		resolvedReportIds := []int64{}
		var resolutionRows []models.ModerationReportResolution
		result := s.db.Where("action_id = ?", row.ID).Find(&resolutionRows)
		if result.Error != nil {
			return nil, result.Error
		}
		for _, row := range resolutionRows {
			resolvedReportIds = append(resolvedReportIds, int64(row.ReportId))
		}

		subjectBlobCIDs := []string{}
		var cidRows []models.ModerationActionSubjectBlobCid
		result = s.db.Where("action_id = ?", row.ID).Find(&cidRows)
		if result.Error != nil {
			return nil, result.Error
		}
		for _, row := range cidRows {
			subjectBlobCIDs = append(subjectBlobCIDs, row.Cid)
		}

		var reversal *comatproto.AdminDefs_ActionReversal
		if row.ReversedAt != nil {
			reversal = &comatproto.AdminDefs_ActionReversal{
				CreatedAt: row.ReversedAt.Format(time.RFC3339),
				CreatedBy: *row.ReversedByDid,
				Reason:    *row.ReversedReason,
			}
		}
		var subj *comatproto.AdminDefs_ActionView_Subject
		switch row.SubjectType {
		case "com.atproto.repo.repoRef":
			subj = &comatproto.AdminDefs_ActionView_Subject{
				AdminDefs_RepoRef: &comatproto.AdminDefs_RepoRef{
					LexiconTypeID: "com.atproto.repo.repoRef",
					Did:           row.SubjectDid,
				},
			}
		case "com.atproto.repo.recordRef":
			subj = &comatproto.AdminDefs_ActionView_Subject{
				RepoStrongRef: &comatproto.RepoStrongRef{
					LexiconTypeID: "com.atproto.repo.strongRef",
					Uri:           *row.SubjectUri,
					Cid:           *row.SubjectCid,
				},
			}
		default:
			return nil, fmt.Errorf("unsupported moderation SubjectType: %v", row.SubjectType)
		}

		view := &comatproto.AdminDefs_ActionView{
			Action:            &row.Action,
			CreatedAt:         row.CreatedAt.Format(time.RFC3339),
			CreatedBy:         row.CreatedByDid,
			Id:                int64(row.ID),
			Reason:            row.Reason,
			ResolvedReportIds: resolvedReportIds,
			Reversal:          reversal,
			Subject:           subj,
			SubjectBlobCids:   subjectBlobCIDs,
		}
		out = append(out, view)
	}
	return out, nil
}

func (s *Server) hydrateModerationActionDetails(ctx context.Context, rows []models.ModerationAction) ([]*comatproto.AdminDefs_ActionViewDetail, error) {

	var out []*comatproto.AdminDefs_ActionViewDetail
	for _, row := range rows {

		var reportRows []models.ModerationReport
		result := s.db.Joins("left join moderation_report_resolutions on moderation_report_resolutions.report_id = moderation_reports.id").Where("moderation_report_resolutions.action_id = ?", row.ID).Find(&reportRows)
		if result.Error != nil {
			return nil, result.Error
		}
		resolvedReports, err := s.hydrateModerationReportViews(ctx, reportRows)
		if err != nil {
			return nil, err
		}

		subjectBlobViews := []*comatproto.AdminDefs_BlobView{}
		var cidRows []models.ModerationActionSubjectBlobCid
		result = s.db.Where("action_id = ?", row.ID).Find(&cidRows)
		if result.Error != nil {
			return nil, result.Error
		}
		for _, row := range cidRows {
			subjectBlobViews = append(subjectBlobViews, &comatproto.AdminDefs_BlobView{
				Cid: row.Cid,
				/* TODO(bnewbold): all these other blob fields (from another backed)
				CreatedAt     string
				Details       *AdminDefs_BlobView_Details
				MimeType      string
				Moderation    *AdminDefs_Moderation
				Size          int64
				*/
			})
		}

		var reversal *comatproto.AdminDefs_ActionReversal
		if row.ReversedAt != nil {
			reversal = &comatproto.AdminDefs_ActionReversal{
				CreatedAt: row.ReversedAt.Format(time.RFC3339),
				CreatedBy: *row.ReversedByDid,
				Reason:    *row.ReversedReason,
			}
		}
		var subj *comatproto.AdminDefs_ActionViewDetail_Subject
		switch row.SubjectType {
		case "com.atproto.repo.repoRef":
			subj = &comatproto.AdminDefs_ActionViewDetail_Subject{
				AdminDefs_RepoView: s.hydrateRepoView(ctx, row.SubjectDid, row.CreatedAt.Format(time.RFC3339)),
			}
		case "com.atproto.repo.recordRef":
			subj = &comatproto.AdminDefs_ActionViewDetail_Subject{
				AdminDefs_RecordView: s.hydrateRecordView(ctx, row.SubjectDid, row.SubjectUri, row.SubjectCid, row.CreatedAt.Format(time.RFC3339)),
			}
		default:
			return nil, fmt.Errorf("unsupported moderation SubjectType: %v", row.SubjectType)
		}

		viewDetail := &comatproto.AdminDefs_ActionViewDetail{
			Action:          &row.Action,
			CreatedAt:       row.CreatedAt.Format(time.RFC3339),
			CreatedBy:       row.CreatedByDid,
			Id:              int64(row.ID),
			Reason:          row.Reason,
			ResolvedReports: resolvedReports,
			Reversal:        reversal,
			Subject:         subj,
			SubjectBlobs:    subjectBlobViews,
		}
		out = append(out, viewDetail)
	}
	return out, nil
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
			Reason:              row.Reason,
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
		resolvedByActionViews, err := s.hydrateModerationActionViews(ctx, actionRows)
		if err != nil {
			return nil, err
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
			Id:                int64(row.ID),
			Reason:            row.Reason,
			ReasonType:        &row.ReasonType,
			Subject:           subj,
			ReportedBy:        row.ReportedByDid,
			CreatedAt:         row.CreatedAt.Format(time.RFC3339),
			ResolvedByActions: resolvedByActionViews,
		}
		out = append(out, viewDetail)
	}
	return out, nil
}
