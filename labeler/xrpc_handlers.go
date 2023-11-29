package labeler

import (
	"context"
	"strconv"
	"strings"
	"time"

	atproto "github.com/bluesky-social/indigo/api/atproto"
	label "github.com/bluesky-social/indigo/api/label"
	"github.com/bluesky-social/indigo/models"
	"github.com/bluesky-social/indigo/util"

	"github.com/labstack/echo/v4"
)

func (s *Server) handleComAtprotoServerDescribeServer(ctx context.Context) (*atproto.ServerDescribeServer_Output, error) {
	invcode := true
	return &atproto.ServerDescribeServer_Output{
		InviteCodeRequired:   &invcode,
		AvailableUserDomains: []string{},
		Links:                &atproto.ServerDescribeServer_Links{},
	}, nil
}

func (s *Server) handleComAtprotoLabelQueryLabels(ctx context.Context, cursor string, limit int, sources, uriPatterns []string) (*label.QueryLabels_Output, error) {

	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	q := s.db.Limit(limit).Order("id desc")
	if cursor != "" {
		cursorID, err := strconv.Atoi(cursor)
		if err != nil {
			return nil, err
		}
		q = q.Where("id < ?", cursorID)
	}

	srcQuery := s.db
	for _, src := range sources {
		if src == "*" {
			continue
		}
		srcQuery = srcQuery.Or("source_did = ?", src)
	}
	if srcQuery != s.db {
		q = q.Where(srcQuery)
	}

	uriQuery := s.db
	for _, pat := range uriPatterns {
		if strings.HasSuffix(pat, "*") {
			likePat := []rune(pat)
			likePat[len(likePat)-1] = '%'
			uriQuery = uriQuery.Or("uri LIKE ?", string(likePat))
		} else {
			uriQuery = uriQuery.Or("uri = ?", pat)
		}
	}
	if uriQuery != s.db {
		q = q.Where(uriQuery)
	}

	var labelRows []models.Label
	result := q.Find(&labelRows)
	if result.Error != nil {
		return nil, result.Error
	}

	var nextCursor string
	if len(labelRows) >= 1 && len(labelRows) == limit {
		nextCursor = strconv.FormatUint(labelRows[len(labelRows)-1].ID, 10)
	}

	labelObjs := []*label.Label{}
	for _, row := range labelRows {
		neg := false
		if row.Neg != nil && *row.Neg == true {
			neg = true
		}
		labelObjs = append(labelObjs, &label.Label{
			Src: row.SourceDid,
			Uri: row.Uri,
			Cid: row.Cid,
			Val: row.Val,
			Neg: neg,
			Cts: row.CreatedAt.Format(util.ISO8601),
		})
	}
	out := label.QueryLabels_Output{
		Labels: labelObjs,
	}
	if nextCursor != "" {
		out.Cursor = &nextCursor
	}
	return &out, nil
}

func (s *Server) handleComAtprotoAdminGetModerationReport(ctx context.Context, id int) (*atproto.AdminDefs_ReportViewDetail, error) {

	var row models.ModerationReport
	result := s.db.First(&row, id)
	if result.Error != nil {
		return nil, result.Error
	}

	full, err := s.hydrateModerationReportDetails(ctx, []models.ModerationReport{row})
	if err != nil {
		return nil, err
	}
	return full[0], nil
}

func didFromURI(uri string) string {
	parts := strings.SplitN(uri, "/", 4)
	if len(parts) < 3 {
		return ""
	}
	if strings.HasPrefix(parts[2], "did:") {
		return parts[2]
	}
	return ""
}

func (s *Server) handleComAtprotoReportCreate(ctx context.Context, body *atproto.ModerationCreateReport_Input) (*atproto.ModerationCreateReport_Output, error) {

	if body.ReasonType == nil || *body.ReasonType == "" {
		return nil, echo.NewHTTPError(400, "reasonType is required")
	}
	if body.Subject == nil {
		return nil, echo.NewHTTPError(400, "Subject is required")
	}

	row := models.ModerationReport{
		ReasonType: *body.ReasonType,
		Reason:     body.Reason,
		// TODO(bnewbold): temporarily, all reports from labelmaker user
		ReportedByDid: s.user.Did,
	}
	var outSubj atproto.ModerationCreateReport_Output_Subject
	if body.Subject.AdminDefs_RepoRef != nil {
		if body.Subject.AdminDefs_RepoRef.Did == "" {
			return nil, echo.NewHTTPError(400, "DID is required for repo reports")
		}
		row.SubjectType = "com.atproto.repo.repoRef"
		row.SubjectDid = body.Subject.AdminDefs_RepoRef.Did
		outSubj.AdminDefs_RepoRef = &atproto.AdminDefs_RepoRef{
			LexiconTypeID: "com.atproto.repo.repoRef",
			Did:           row.SubjectDid,
		}
	} else if body.Subject.RepoStrongRef != nil {
		if body.Subject.RepoStrongRef.Uri == "" {
			return nil, echo.NewHTTPError(400, "URI required for record reports")
		}
		if body.Subject.RepoStrongRef.Cid == "" {
			return nil, echo.NewHTTPError(400, "this implementation requires a strong record ref (aka, with CID) in reports")
		}
		row.SubjectType = "com.atproto.repo.recordRef"
		row.SubjectUri = &body.Subject.RepoStrongRef.Uri
		row.SubjectDid = didFromURI(body.Subject.RepoStrongRef.Uri)
		if row.SubjectDid == "" {
			return nil, echo.NewHTTPError(400, "expected URI with a DID: ", row.SubjectUri)
		}
		row.SubjectCid = &body.Subject.RepoStrongRef.Cid
		outSubj.RepoStrongRef = &atproto.RepoStrongRef{
			LexiconTypeID: "com.atproto.repo.strongRef",
			Uri:           *row.SubjectUri,
			Cid:           *row.SubjectCid,
		}
	} else {
		return nil, echo.NewHTTPError(400, "report subject must be a repoRef or a recordRef")
	}

	result := s.db.Create(&row)
	if result.Error != nil {
		return nil, result.Error
	}

	out := atproto.ModerationCreateReport_Output{
		Id:         int64(row.ID),
		CreatedAt:  row.CreatedAt.Format(time.RFC3339),
		Reason:     row.Reason,
		ReasonType: &row.ReasonType,
		ReportedBy: row.ReportedByDid,
		Subject:    &outSubj,
	}
	return &out, nil
}
