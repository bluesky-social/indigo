package labeling

import (
	"context"
	"strconv"
	"strings"
	"time"
	"errors"

	atproto "github.com/bluesky-social/indigo/api/atproto"
	label "github.com/bluesky-social/indigo/api/label"
	"github.com/bluesky-social/indigo/models"
	"github.com/bluesky-social/indigo/util"

	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (s *Server) handleComAtprotoIdentityResolveHandle(ctx context.Context, handle string) (*atproto.IdentityResolveHandle_Output, error) {
	// only the one handle, for labelmaker
	if handle == "" {
		return &atproto.IdentityResolveHandle_Output{Did: s.user.SigningKey.Public().DID()}, nil
	} else if handle == s.user.Handle {
		return &atproto.IdentityResolveHandle_Output{Did: s.user.Did}, nil
	} else {
		return nil, echo.NewHTTPError(404, "user not found: %s", handle)
	}
}

func (s *Server) handleComAtprotoServerDescribeServer(ctx context.Context) (*atproto.ServerDescribeServer_Output, error) {
	invcode := true
	return &atproto.ServerDescribeServer_Output{
		InviteCodeRequired:   &invcode,
		AvailableUserDomains: []string{},
		Links:                &atproto.ServerDescribeServer_Links{},
	}, nil
}

func (s *Server) handleComAtprotoLabelQuery(ctx context.Context, cursor string, limit int, sources, uriPatterns []string) (*label.Query_Output, error) {

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
		labelObjs = append(labelObjs, &label.Label{
			Src: row.SourceDid,
			Uri: row.Uri,
			Cid: row.Cid,
			Val: row.Val,
			Cts: row.CreatedAt.Format(util.ISO8601),
		})
	}
	out := label.Query_Output{
		Labels: labelObjs,
	}
	if nextCursor != "" {
		out.Cursor = &nextCursor
	}
	return &out, nil
}

func (s *Server) handleComAtprotoAdminGetModerationAction(ctx context.Context, id int) (*atproto.AdminDefs_ActionViewDetail, error) {

	var row models.ModerationAction
	result := s.db.First(&row, id)
	if result.Error != nil {
		return nil, result.Error
	}

	full, err := s.hydrateModerationActionDetails(ctx, []models.ModerationAction{row})
	if err != nil {
		return nil, err
	}
	return full[0], nil
}

func (s *Server) handleComAtprotoAdminGetModerationActions(ctx context.Context, before string, limit int, subject string) (*atproto.AdminGetModerationActions_Output, error) {

	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	q := s.db.Limit(limit).Order("id desc")
	cursor := before
	if cursor != "" {
		cursorID, err := strconv.Atoi(cursor)
		if err != nil {
			return nil, echo.NewHTTPError(400, "invalid cursor param: %v", cursor)
		}
		q = q.Where("id < ?", cursorID)
	}

	if subject != "" {
		q = q.Where("subject = ?", subject)
	}

	var actionRows []models.ModerationAction
	result := q.Find(&actionRows)
	if result.Error != nil {
		return nil, result.Error
	}

	var nextCursor string
	if len(actionRows) >= 1 && len(actionRows) == limit {
		nextCursor = strconv.FormatUint(actionRows[len(actionRows)-1].ID, 10)
	}

	actionObjs, err := s.hydrateModerationActions(ctx, actionRows)
	if err != nil {
		return nil, err
	}
	out := atproto.AdminGetModerationActions_Output{
		Actions: actionObjs,
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

func (s *Server) handleComAtprotoAdminGetModerationReports(ctx context.Context, before string, limit int, resolved *bool, subject string) (*atproto.AdminGetModerationReports_Output, error) {

	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	q := s.db.Limit(limit).Order("id desc")
	cursor := before
	if cursor != "" {
		cursorID, err := strconv.Atoi(cursor)
		if err != nil {
			return nil, echo.NewHTTPError(400, "invalid cursor param: %v", cursor)
		}
		q = q.Where("id < ?", cursorID)
	}

	if subject != "" {
		q = q.Where("subject = ?", subject)
	}

	var reportRows []models.ModerationReport
	result := q.Find(&reportRows)
	if result.Error != nil {
		return nil, result.Error
	}

	var nextCursor string
	if len(reportRows) >= 1 && len(reportRows) == limit {
		nextCursor = strconv.FormatUint(reportRows[len(reportRows)-1].ID, 10)
	}

	reportObjs, err := s.hydrateModerationReports(ctx, reportRows)
	if err != nil {
		return nil, err
	}

	// TODO: a bit inefficient to do this filter after hydration. could do it
	// in the SQL query instead, but this was faster to implement right now
	if resolved != nil {
		var filtered []*atproto.AdminDefs_ReportView
		for _, obj := range reportObjs {
			if *resolved == true && len(obj.ResolvedByActionIds) > 0 {
				filtered = append(filtered, obj)
			}
			if *resolved == false && len(obj.ResolvedByActionIds) == 0 {
				filtered = append(filtered, obj)
			}
		}
		reportObjs = filtered
	}

	out := atproto.AdminGetModerationReports_Output{
		Reports: reportObjs,
	}
	if nextCursor != "" {
		out.Cursor = &nextCursor
	}
	return &out, nil
}

func (s *Server) handleComAtprotoAdminResolveModerationReports(ctx context.Context, body *atproto.AdminResolveModerationReports_Input) (*atproto.AdminDefs_ActionView, error) {

	if body.CreatedBy == "" {
		return nil, echo.NewHTTPError(400, "createdBy param must be non-empty")
	}
	if len(body.ReportIds) == 0 {
		return nil, echo.NewHTTPError(400, "at least one reportId required")
	}

	var rows []models.ModerationReportResolution
	for _, reportId := range body.ReportIds {
		rows = append(rows, models.ModerationReportResolution{
			ReportId:     uint64(reportId),
			ActionId:     uint64(body.ActionId),
			CreatedByDid: body.CreatedBy,
		})
	}
	result := s.db.Create(&rows)
	if result.Error != nil {
		return nil, result.Error
	}

	return s.fetchSingleModerationAction(ctx, body.ActionId)
}

// helper for endpoints that return a partially hydrated moderation action
func (s *Server) fetchSingleModerationAction(ctx context.Context, actionId int64) (*atproto.AdminDefs_ActionView, error) {
	var actionRow models.ModerationAction
	result := s.db.First(&actionRow, actionId)
	if result.Error != nil {
		return nil, result.Error
	}

	actionObjs, err := s.hydrateModerationActions(ctx, []models.ModerationAction{actionRow})
	if err != nil {
		return nil, err
	}
	return actionObjs[0], nil
}

func (s *Server) handleComAtprotoAdminReverseModerationAction(ctx context.Context, body *atproto.AdminReverseModerationAction_Input) (*atproto.AdminDefs_ActionView, error) {

	if body.CreatedBy == "" {
		return nil, echo.NewHTTPError(400, "createBy param must be non-empty")
	}
	if body.Reason == "" {
		return nil, echo.NewHTTPError(400, "reason param was provided, but empty string")
	}

	row := models.ModerationAction{ID: uint64(body.Id)}
	result := s.db.First(&row)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, echo.NewHTTPError(404, "moderation action not found: %d", body.Id)
		} else {
			return nil, result.Error
		}
	}

	if row.ReversedAt != nil {
		return nil, echo.NewHTTPError(400, "action has already been reversed actionId=%d", body.Id)
	}

	now := time.Now()
	row.ReversedByDid = &body.CreatedBy
	row.ReversedReason = &body.Reason
	row.ReversedAt = &now

	result = s.db.Save(&row)
	if result.Error != nil {
		return nil, result.Error
	}

	return s.fetchSingleModerationAction(ctx, body.Id)
}

func (s *Server) handleComAtprotoAdminTakeModerationAction(ctx context.Context, body *atproto.AdminTakeModerationAction_Input) (*atproto.AdminDefs_ActionView, error) {

	if body.Action == "" {
		return nil, echo.NewHTTPError(400, "action param must be non-empty")
	}
	if body.CreatedBy == "" {
		return nil, echo.NewHTTPError(400, "createBy param must be non-empty")
	}
	if body.Reason == "" {
		return nil, echo.NewHTTPError(400, "reason param was provided, but empty string")
	}

	// XXX: SubjectBlobCids (how does atproto do it? array in postgresql?)
	row := models.ModerationAction{
		Action:       body.Action,
		Reason:       body.Reason,
		CreatedByDid: body.CreatedBy,
	}

	var outSubj atproto.AdminDefs_ActionView_Subject
	if body.Subject.AdminDefs_RepoRef != nil {
		row.SubjectType = "com.atproto.repo.repoRef"
		row.SubjectDid = body.Subject.AdminDefs_RepoRef.Did
		outSubj.AdminDefs_RepoRef = &atproto.AdminDefs_RepoRef{
			LexiconTypeID: "com.atproto.repo.repoRef",
			Did:           row.SubjectDid,
		}
	} else if body.Subject.RepoStrongRef != nil {
		if body.Subject.RepoStrongRef.Cid == "" {
			return nil, echo.NewHTTPError(400, "this implementation requires a strong record ref (aka, with CID) in reports")
		}
		row.SubjectType = "com.atproto.repo.recordRef"
		// TODO: row.SubjectDid from URI?
		row.SubjectUri = &body.Subject.RepoStrongRef.Uri
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

	out := atproto.AdminDefs_ActionView{
		Id:        int64(row.ID),
		Action:    &row.Action,
		Reason:    row.Reason,
		CreatedBy: row.CreatedByDid,
		CreatedAt: row.CreatedAt.Format(time.RFC3339),
		Subject:   &outSubj,
		// XXX: SubjectBlobCids
	}
	return &out, nil
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
		// TODO(bnewbold): from auth, via context? as a new lexicon field?
		ReportedByDid: "did:plc:FAKE",
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
		// TODO: row.SubjectDid from URI?
		row.SubjectUri = &body.Subject.RepoStrongRef.Uri
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
		Subject:    &outSubj,
	}
	return &out, nil
}
