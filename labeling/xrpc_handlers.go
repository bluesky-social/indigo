package labeling

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	atproto "github.com/bluesky-social/indigo/api/atproto"
	label "github.com/bluesky-social/indigo/api/label"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/models"
	"github.com/bluesky-social/indigo/util"

	"github.com/ipfs/go-cid"
	"github.com/labstack/echo/v4"
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

func (s *Server) handleComAtprotoRepoDescribeRepo(ctx context.Context, repo string) (*atproto.RepoDescribeRepo_Output, error) {
	if user == s.user.Did || user == s.user.Handle {
		return &atproto.RepoDescribe_Output{
			Collections: []string{},
			Did:         s.user.Did,
			//DidDoc
			Handle:          s.user.Handle,
			HandleIsCorrect: true,
		}, nil
	}
	if user == "" {
		return nil, echo.NewHTTPError(400, "empty user parameter")
	} else {
		return nil, echo.NewHTTPError(404, "user not found")
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

func (s *Server) handleComAtprotoSyncGetRepo(ctx context.Context, did string, earliest, latest string) (io.Reader, error) {
	// TODO: verify the DID/handle
	var userId util.Uid = 1
	var earlyCid cid.Cid
	if earliest != "" {
		cc, err := cid.Decode(earliest)
		if err != nil {
			return nil, err
		}

		earlyCid = cc
	}

	var lateCid cid.Cid
	if latest != "" {
		cc, err := cid.Decode(latest)
		if err != nil {
			return nil, err
		}

		lateCid = cc
	}

	buf := new(bytes.Buffer)
	if err := s.repoman.ReadRepo(ctx, userId, earlyCid, lateCid, buf); err != nil {
		return nil, err
	}

	return buf, nil
}

func (s *Server) handleComAtprotoRepoGetRecord(ctx context.Context, c string, collection string, rkey string, user string) (*atproto.RepoGetRecord_Output, error) {
	if user != s.user.Did {
		return nil, fmt.Errorf("unknown user: %s", user)
	}

	var maybeCid cid.Cid
	if c != "" {
		cc, err := cid.Decode(c)
		if err != nil {
			return nil, err
		}
		maybeCid = cc
	}

	reccid, rec, err := s.repoman.GetRecord(ctx, s.user.UserId, collection, rkey, maybeCid)
	if err != nil {
		return nil, fmt.Errorf("repoman GetRecord: %w", err)
	}

	ccstr := reccid.String()
	return &atproto.RepoGetRecord_Output{
		Cid:   &ccstr,
		Uri:   "at://" + s.user.Did + "/" + collection + "/" + rkey,
		Value: &lexutil.LexiconTypeDecoder{rec},
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
	fmt.Printf("%v\n", sources)
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

func (s *Server) handleComAtprotoAdminGetModerationAction(ctx context.Context, id int) (*atproto.AdminModerationAction_ViewDetail, error) {

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
			// XXX: HTTP 400 error
			return nil, err
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

func (s *Server) handleComAtprotoAdminGetModerationReport(ctx context.Context, id int) (*atproto.AdminModerationReport_ViewDetail, error) {

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
			// XXX: HTTP 400 error
			return nil, err
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
		var filtered []*atproto.AdminModerationReport_View
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

func (s *Server) handleComAtprotoAdminResolveModerationReports(ctx context.Context, body *atproto.AdminResolveModerationReports_Input) (*atproto.AdminModerationAction_View, error) {

	// XXX: check that body fields are not nil/empty: CreatedBy, ReportIds

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
func (s *Server) fetchSingleModerationAction(ctx context.Context, actionId int64) (*atproto.AdminModerationAction_View, error) {
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

func (s *Server) handleComAtprotoAdminReverseModerationAction(ctx context.Context, body *atproto.AdminReverseModerationAction_Input) (*atproto.AdminModerationAction_View, error) {

	// XXX: validate body CreatedBy, Reason

	row := models.ModerationAction{ID: uint64(body.Id)}
	result := s.db.First(&row)
	if result.Error != nil {
		// XXX: if not found, 404
		return nil, result.Error
	}

	if row.ReversedAt != nil {
		// XXX: http 400 (already reversed)
		return nil, fmt.Errorf("action has already been reversed actionId=%d", body.Id)
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

func (s *Server) handleComAtprotoAdminTakeModerationAction(ctx context.Context, body *atproto.AdminTakeModerationAction_Input) (*atproto.AdminModerationAction_View, error) {

	// XXX: check that Action, CreatedBy, and Reason are all non-empty

	// XXX: SubjectBlobCids (how does atproto do it? array in postgresql?)
	row := models.ModerationAction{
		Action:       body.Action,
		Reason:       body.Reason,
		CreatedByDid: body.CreatedBy,
	}

	var outSubj atproto.AdminModerationAction_View_Subject
	if body.Subject.RepoRepoRef != nil {
		row.SubjectType = "com.atproto.repo.repoRef"
		row.SubjectDid = body.Subject.RepoRepoRef.Did
		outSubj.RepoRepoRef = &atproto.RepoRepoRef{
			LexiconTypeID: "com.atproto.repo.repoRef",
			Did:           row.SubjectDid,
		}
	} else if body.Subject.RepoRecordRef != nil {
		if row.SubjectCid == nil {
			return nil, fmt.Errorf("this implementation requires a strong record ref (aka, with CID) in reports")
		}
		row.SubjectType = "com.atproto.repo.recordRef"
		// TODO: row.SubjectDid from URI?
		row.SubjectUri = &body.Subject.RepoRecordRef.Uri
		row.SubjectCid = body.Subject.RepoRecordRef.Cid
		outSubj.RepoStrongRef = &atproto.RepoStrongRef{
			LexiconTypeID: "com.atproto.repo.strongRef",
			Uri:           *row.SubjectUri,
			Cid:           *row.SubjectCid,
		}
	} else {
		// XXX: 400 error (and similar instances)
		return nil, fmt.Errorf("report subject must be a repoRef or a recordRef")
	}

	result := s.db.Create(&row)
	if result.Error != nil {
		return nil, result.Error
	}

	out := atproto.AdminModerationAction_View{
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

func (s *Server) handleComAtprotoReportCreate(ctx context.Context, body *atproto.ReportCreate_Input) (*atproto.ReportCreate_Output, error) {

	// TODO: shouldn't lexgen and the endpoint handlers help with these already? both are required fields
	if body.ReasonType == nil {
		// XXX: 400 error
		return nil, fmt.Errorf("ReasonType is required")
	}
	if body.Subject == nil {
		// XXX: 400 error
		return nil, fmt.Errorf("Subject is required")
	}

	row := models.ModerationReport{
		ReasonType: *body.ReasonType,
		Reason:     body.Reason,
		// TODO(bnewbold): from auth, via context? as a new lexicon field?
		ReportedByDid: "did:plc:FAKE",
	}
	var outSubj atproto.ReportCreate_Output_Subject
	if body.Subject.RepoRepoRef != nil {
		row.SubjectType = "com.atproto.repo.repoRef"
		row.SubjectDid = body.Subject.RepoRepoRef.Did
		outSubj.RepoRepoRef = &atproto.RepoRepoRef{
			LexiconTypeID: "com.atproto.repo.repoRef",
			Did:           row.SubjectDid,
		}
	} else if body.Subject.RepoRecordRef != nil {
		if row.SubjectCid == nil {
			return nil, fmt.Errorf("this implementation requires a strong record ref (aka, with CID) in reports")
		}
		row.SubjectType = "com.atproto.repo.recordRef"
		// TODO: row.SubjectDid from URI?
		row.SubjectUri = &body.Subject.RepoRecordRef.Uri
		row.SubjectCid = body.Subject.RepoRecordRef.Cid
		outSubj.RepoStrongRef = &atproto.RepoStrongRef{
			LexiconTypeID: "com.atproto.repo.strongRef",
			Uri:           *row.SubjectUri,
			Cid:           *row.SubjectCid,
		}
	} else {
		return nil, fmt.Errorf("report subject must be a repoRef or a recordRef")
	}

	result := s.db.Create(&row)
	if result.Error != nil {
		return nil, result.Error
	}

	out := atproto.ReportCreate_Output{
		Id:         int64(row.ID),
		CreatedAt:  row.CreatedAt.Format(time.RFC3339),
		Reason:     row.Reason,
		ReasonType: &row.ReasonType,
		Subject:    &outSubj,
	}
	return &out, nil
}
