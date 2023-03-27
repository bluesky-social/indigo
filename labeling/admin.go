package labeling

import (
	"context"
	"fmt"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/models"
)

// This is probably only a temporary method
func (s *Server) hydrateRepoView(ctx context.Context, did, indexedAt string) *comatproto.AdminRepo_View {
	return &comatproto.AdminRepo_View{
		// TODO(bnewbold): populate more, or more correctly, from some backend?
		Account:        nil,
		Did:            did,
		Handle:         "TODO",
		IndexedAt:      indexedAt,
		Moderation:     nil,
		RelatedRecords: nil,
	}
}

// This is probably only a temporary method
func (s *Server) hydrateRecordView(ctx context.Context, did string, uri, cid *string, indexedAt string) *comatproto.AdminRecord_View {
	repoView := s.hydrateRepoView(ctx, did, indexedAt)
	// TODO(bnewbold): populate more, or more correctly, from some backend?
	recordView := comatproto.AdminRecord_View{
		BlobCids:   []string{},
		IndexedAt:  indexedAt,
		Moderation: nil,
		Repo:       repoView,
		// TODO: Value
	}
	if uri != nil {
		recordView.Uri = *uri
	}
	if cid != nil {
		recordView.Cid = *cid
	}
	return &recordView
}

func (s *Server) hydrateModerationActions(ctx context.Context, rows []models.ModerationAction) ([]*comatproto.AdminModerationAction_View, error) {

	var out []*comatproto.AdminModerationAction_View

	for _, row := range rows {
		// TODO(bnewbold): resolve these
		resolvedReportIds := []int64{}
		subjectBlobCIDs := []string{}

		var reversal *comatproto.AdminModerationAction_Reversal
		if row.ReversedAt != nil {
			reversal = &comatproto.AdminModerationAction_Reversal{
				CreatedAt: row.ReversedAt.Format(time.RFC3339),
				CreatedBy: *row.ReversedByDid,
				Reason:    *row.ReversedReason,
			}
		}
		var subj *comatproto.AdminModerationAction_View_Subject
		switch row.SubjectType {
		case "com.atproto.repo.repoRef":
			subj = &comatproto.AdminModerationAction_View_Subject{
				RepoRepoRef: &comatproto.RepoRepoRef{
					LexiconTypeID: "com.atproto.repo.repoRef",
					Did:           row.SubjectDid,
				},
			}
		case "com.atproto.repo.recordRef":
			subj = &comatproto.AdminModerationAction_View_Subject{
				RepoStrongRef: &comatproto.RepoStrongRef{
					LexiconTypeID: "com.atproto.repo.strongRef",
					Uri:           *row.SubjectUri,
					Cid:           *row.SubjectCid,
				},
			}
		default:
			return nil, fmt.Errorf("unsupported moderation SubjectType: %v", row.SubjectType)
		}

		view := &comatproto.AdminModerationAction_View{
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

func (s *Server) hydrateModerationActionDetails(ctx context.Context, rows []models.ModerationAction) ([]*comatproto.AdminModerationAction_ViewDetail, error) {

	var out []*comatproto.AdminModerationAction_ViewDetail
	for _, row := range rows {

		// TODO(bnewbold): resolve these
		resolvedReports := []*comatproto.AdminModerationReport_View{}
		subjectBlobs := []*comatproto.AdminBlob_View{}

		var reversal *comatproto.AdminModerationAction_Reversal
		if row.ReversedAt != nil {
			reversal = &comatproto.AdminModerationAction_Reversal{
				CreatedAt: row.ReversedAt.Format(time.RFC3339),
				CreatedBy: *row.ReversedByDid,
				Reason:    *row.ReversedReason,
			}
		}
		var subj *comatproto.AdminModerationAction_ViewDetail_Subject
		switch row.SubjectType {
		case "com.atproto.repo.repoRef":
			subj = &comatproto.AdminModerationAction_ViewDetail_Subject{
				AdminRepo_View: s.hydrateRepoView(ctx, row.SubjectDid, row.CreatedAt.Format(time.RFC3339)),
			}
		case "com.atproto.repo.recordRef":
			subj = &comatproto.AdminModerationAction_ViewDetail_Subject{
				AdminRecord_View: s.hydrateRecordView(ctx, row.SubjectDid, row.SubjectUri, row.SubjectCid, row.CreatedAt.Format(time.RFC3339)),
			}
		default:
			return nil, fmt.Errorf("unsupported moderation SubjectType: %v", row.SubjectType)
		}

		viewDetail := &comatproto.AdminModerationAction_ViewDetail{
			Action:          &row.Action,
			CreatedAt:       row.CreatedAt.Format(time.RFC3339),
			CreatedBy:       row.CreatedByDid,
			Id:              int64(row.ID),
			Reason:          row.Reason,
			ResolvedReports: resolvedReports,
			Reversal:        reversal,
			Subject:         subj,
			SubjectBlobs:    subjectBlobs,
		}
		out = append(out, viewDetail)
	}
	return out, nil
}

func (s *Server) hydrateModerationReports(ctx context.Context, rows []models.ModerationReport) ([]*comatproto.AdminModerationReport_View, error) {

	var out []*comatproto.AdminModerationReport_View
	for _, row := range rows {
		// TODO(bnewbold): fetch these IDs
		var resolvedByActionIds []int64

		var subj *comatproto.AdminModerationReport_View_Subject
		switch row.SubjectType {
		case "com.atproto.repo.repoRef":
			subj = &comatproto.AdminModerationReport_View_Subject{
				RepoRepoRef: &comatproto.RepoRepoRef{
					LexiconTypeID: "com.atproto.repo.repoRef",
					Did:           row.SubjectDid,
				},
			}
		case "com.atproto.repo.recordRef":
			subj = &comatproto.AdminModerationReport_View_Subject{
				RepoStrongRef: &comatproto.RepoStrongRef{
					LexiconTypeID: "com.atproto.repo.strongRef",
					Uri:           *row.SubjectUri,
					Cid:           *row.SubjectCid,
				},
			}
		default:
			return nil, fmt.Errorf("unsupported moderation SubjectType: %v", row.SubjectType)
		}

		view := &comatproto.AdminModerationReport_View{
			Id:                  int64(row.ID),
			ReasonType:          &row.ReasonType,
			Subject:             subj,
			ReportedByDid:       row.ReportedByDid,
			CreatedAt:           row.CreatedAt.Format(time.RFC3339),
			ResolvedByActionIds: resolvedByActionIds,
		}
		out = append(out, view)
	}
	return out, nil
}

func (s *Server) hydrateModerationReportDetails(ctx context.Context, rows []models.ModerationReport) ([]*comatproto.AdminModerationReport_ViewDetail, error) {

	var out []*comatproto.AdminModerationReport_ViewDetail
	for _, row := range rows {
		// TODO(bnewbold): fetch these objects
		var resolvedByActions []*comatproto.AdminModerationAction_View

		var subj *comatproto.AdminModerationReport_ViewDetail_Subject
		switch row.SubjectType {
		case "com.atproto.repo.repoRef":
			subj = &comatproto.AdminModerationReport_ViewDetail_Subject{
				AdminRepo_View: s.hydrateRepoView(ctx, row.SubjectDid, row.CreatedAt.Format(time.RFC3339)),
			}
		case "com.atproto.repo.recordRef":
			subj = &comatproto.AdminModerationReport_ViewDetail_Subject{
				AdminRecord_View: s.hydrateRecordView(ctx, row.SubjectDid, row.SubjectUri, row.SubjectCid, row.CreatedAt.Format(time.RFC3339)),
			}
		default:
			return nil, fmt.Errorf("unsupported moderation SubjectType: %v", row.SubjectType)
		}

		viewDetail := &comatproto.AdminModerationReport_ViewDetail{
			Id:                int64(row.ID),
			ReasonType:        &row.ReasonType,
			Subject:           subj,
			ReportedByDid:     row.ReportedByDid,
			CreatedAt:         row.CreatedAt.Format(time.RFC3339),
			ResolvedByActions: resolvedByActions,
		}
		out = append(out, viewDetail)
	}
	return out, nil
}
