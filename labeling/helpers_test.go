package labeling

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
)

// fetches report, both getModerationReport and getModerationReports, verifies match
func testGetReport(t *testing.T, e *echo.Echo, lm *Server, reportId int64) comatproto.AdminDefs_ReportViewDetail {
	assert := assert.New(t)

	params := make(url.Values)
	params.Set("id", strconv.Itoa(int(reportId)))
	req := httptest.NewRequest(http.MethodGet, "/xrpc/com.atproto.admin.getModerationReport?"+params.Encode(), nil)
	recorder := httptest.NewRecorder()
	c := e.NewContext(req, recorder)
	assert.NoError(lm.HandleComAtprotoAdminGetModerationReport(c))
	assert.Equal(200, recorder.Code)
	var reportViewDetail comatproto.AdminDefs_ReportViewDetail
	if err := json.Unmarshal([]byte(recorder.Body.String()), &reportViewDetail); err != nil {
		t.Fatal(err)
	}
	assert.Equal(reportId, reportViewDetail.Id)

	// read back (getModerationReports) and verify output
	// TODO: include 'subject' param
	req = httptest.NewRequest(http.MethodGet, "/xrpc/com.atproto.admin.getModerationReports", nil)
	recorder = httptest.NewRecorder()
	c = e.NewContext(req, recorder)
	assert.NoError(lm.HandleComAtprotoAdminGetModerationReports(c))
	assert.Equal(200, recorder.Code)
	var reportsOut comatproto.AdminGetModerationReports_Output
	if err := json.Unmarshal([]byte(recorder.Body.String()), &reportsOut); err != nil {
		t.Fatal(err)
	}
	var reportView *comatproto.AdminDefs_ReportView
	for _, rv := range reportsOut.Reports {
		if rv.Id == reportId {
			reportView = rv
			break
		}
	}
	if reportView == nil {
		t.Fatal("expected to find report by subject")
	}

	assert.Equal(reportViewDetail.Id, reportView.Id)
	assert.Equal(reportViewDetail.CreatedAt, reportView.CreatedAt)
	assert.Equal(reportViewDetail.Reason, reportView.Reason)
	assert.Equal(reportViewDetail.ReasonType, reportView.ReasonType)
	assert.Equal(reportViewDetail.ReportedBy, reportView.ReportedBy)
	assert.Equal(len(reportViewDetail.ResolvedByActions), len(reportView.ResolvedByActionIds))
	for i, actionId := range reportView.ResolvedByActionIds {
		assert.Equal(actionId, reportViewDetail.ResolvedByActions[i].Id)
	}
	if reportViewDetail.Subject.AdminDefs_RepoView != nil {
		assert.Equal(reportViewDetail.Subject.AdminDefs_RepoView.Did, reportView.Subject.AdminDefs_RepoRef.Did)
	} else if reportViewDetail.Subject.AdminDefs_RecordView != nil {
		assert.Equal(reportViewDetail.Subject.AdminDefs_RecordView.Uri, reportView.Subject.RepoStrongRef.Uri)
		assert.Equal(reportViewDetail.Subject.AdminDefs_RecordView.Cid, reportView.Subject.RepoStrongRef.Cid)
	} else {
		t.Fatal("expected non-empty reportviewdetail.subject enum")
	}

	return reportViewDetail
}

// "happy path" test helper. creates a report, reads it back 2x ways, verifies match, then returns the original output
func testCreateReport(t *testing.T, e *echo.Echo, lm *Server, input *comatproto.ModerationCreateReport_Input) comatproto.ModerationCreateReport_Output {
	assert := assert.New(t)

	// create report and verify output
	reportJSON, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/xrpc/com.atproto.report.create", strings.NewReader(string(reportJSON)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	recorder := httptest.NewRecorder()
	c := e.NewContext(req, recorder)

	assert.NoError(lm.HandleComAtprotoReportCreate(c))
	assert.Equal(200, recorder.Code)

	var out comatproto.ModerationCreateReport_Output
	if err := json.Unmarshal([]byte(recorder.Body.String()), &out); err != nil {
		t.Fatal(err)
	}
	assert.Equal(input.Reason, out.Reason)
	assert.Equal(input.ReasonType, out.ReasonType)
	assert.Equal(input.Subject.RepoStrongRef, out.Subject.RepoStrongRef)
	assert.Equal(input.Subject.AdminDefs_RepoRef, out.Subject.AdminDefs_RepoRef)

	// read it back and verify output
	reportViewDetail := testGetReport(t, e, lm, out.Id)
	assert.Equal(out.Id, reportViewDetail.Id)
	assert.Equal(out.CreatedAt, reportViewDetail.CreatedAt)
	assert.Equal(out.ReportedBy, reportViewDetail.ReportedBy)
	assert.Equal(out.Reason, reportViewDetail.Reason)
	assert.Equal(out.ReasonType, reportViewDetail.ReasonType)
	assert.Equal(0, len(reportViewDetail.ResolvedByActions))
	if out.Subject.AdminDefs_RepoRef != nil {
		assert.Equal(out.Subject.AdminDefs_RepoRef.Did, reportViewDetail.Subject.AdminDefs_RepoView.Did)
	} else if out.Subject.RepoStrongRef != nil {
		assert.Equal(out.Subject.RepoStrongRef.Uri, reportViewDetail.Subject.AdminDefs_RecordView.Uri)
		assert.Equal(out.Subject.RepoStrongRef.Cid, reportViewDetail.Subject.AdminDefs_RecordView.Cid)
	} else {
		t.Fatal("expected non-empty actionviewdetail.subject enum")
	}

	return out
}

// fetches action, both getModerationAction and getModerationActions; verifies match
func testGetAction(t *testing.T, e *echo.Echo, lm *Server, actionId int64) comatproto.AdminDefs_ActionViewDetail {
	assert := assert.New(t)

	params := make(url.Values)
	params.Set("id", strconv.Itoa(int(actionId)))
	req := httptest.NewRequest(http.MethodGet, "/xrpc/com.atproto.admin.getModerationAction?"+params.Encode(), nil)
	recorder := httptest.NewRecorder()
	c := e.NewContext(req, recorder)
	assert.NoError(lm.HandleComAtprotoAdminGetModerationAction(c))
	assert.Equal(200, recorder.Code)
	var actionViewDetail comatproto.AdminDefs_ActionViewDetail
	if err := json.Unmarshal([]byte(recorder.Body.String()), &actionViewDetail); err != nil {
		t.Fatal(err)
	}
	assert.Equal(actionId, actionViewDetail.Id)

	// read back (getModerationActions) and verify output
	// TODO: include 'subject' param
	req = httptest.NewRequest(http.MethodGet, "/xrpc/com.atproto.admin.getModerationActions", nil)
	recorder = httptest.NewRecorder()
	c = e.NewContext(req, recorder)
	assert.NoError(lm.HandleComAtprotoAdminGetModerationActions(c))
	assert.Equal(200, recorder.Code)
	var actionsOut comatproto.AdminGetModerationActions_Output
	if err := json.Unmarshal([]byte(recorder.Body.String()), &actionsOut); err != nil {
		t.Fatal(err)
	}
	var actionView *comatproto.AdminDefs_ActionView
	for _, rv := range actionsOut.Actions {
		if rv.Id == actionId {
			actionView = rv
			break
		}
	}
	if actionView == nil {
		t.Fatal("expected to find action by subject")
	}

	assert.Equal(actionViewDetail.Id, actionView.Id)
	assert.Equal(actionViewDetail.CreatedAt, actionView.CreatedAt)
	assert.Equal(actionViewDetail.Action, actionView.Action)
	assert.Equal(actionViewDetail.Reason, actionView.Reason)
	assert.Equal(actionViewDetail.CreatedBy, actionView.CreatedBy)
	assert.Equal(actionViewDetail.Reversal, actionView.Reversal)
	assert.Equal(len(actionViewDetail.ResolvedReports), len(actionView.ResolvedReportIds))
	for i, reportId := range actionView.ResolvedReportIds {
		assert.Equal(reportId, actionViewDetail.ResolvedReports[i].Id)
	}
	for i, blobCid := range actionView.SubjectBlobCids {
		assert.Equal(blobCid, actionViewDetail.SubjectBlobs[i].Cid)
	}
	if actionViewDetail.Subject.AdminDefs_RepoView != nil {
		assert.Equal(actionViewDetail.Subject.AdminDefs_RepoView.Did, actionView.Subject.AdminDefs_RepoRef.Did)
	} else if actionViewDetail.Subject.AdminDefs_RecordView != nil {
		assert.Equal(actionViewDetail.Subject.AdminDefs_RecordView.Uri, actionView.Subject.RepoStrongRef.Uri)
		assert.Equal(actionViewDetail.Subject.AdminDefs_RecordView.Cid, actionView.Subject.RepoStrongRef.Cid)
	} else {
		t.Fatal("expected non-empty actionviewdetail.subject enum")
	}

	return actionViewDetail
}

// "happy path" test helper. creates a action, reads it back 2x ways, verifies match, then returns the original output
func testCreateAction(t *testing.T, e *echo.Echo, lm *Server, input *comatproto.AdminTakeModerationAction_Input) comatproto.AdminDefs_ActionView {
	assert := assert.New(t)

	// create action and verify output
	actionJSON, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/xrpc/com.atproto.action.create", strings.NewReader(string(actionJSON)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	recorder := httptest.NewRecorder()
	c := e.NewContext(req, recorder)

	assert.NoError(lm.HandleComAtprotoAdminTakeModerationAction(c))
	assert.Equal(200, recorder.Code)

	var out comatproto.AdminDefs_ActionView
	if err := json.Unmarshal([]byte(recorder.Body.String()), &out); err != nil {
		t.Fatal(err)
	}
	assert.Equal(input.Action, *out.Action)
	assert.Equal(input.CreatedBy, out.CreatedBy)
	assert.Equal(input.Reason, out.Reason)
	assert.Equal(input.Subject.RepoStrongRef, out.Subject.RepoStrongRef)
	assert.Equal(input.Subject.AdminDefs_RepoRef, out.Subject.AdminDefs_RepoRef)
	assert.Equal(input.SubjectBlobCids, out.SubjectBlobCids)

	// read it back and verify output
	actionViewDetail := testGetAction(t, e, lm, out.Id)
	assert.Equal(out.Id, actionViewDetail.Id)
	assert.Equal(out.CreatedAt, actionViewDetail.CreatedAt)

	assert.Equal(out.Action, actionViewDetail.Action)
	assert.Equal(out.CreatedBy, actionViewDetail.CreatedBy)
	assert.Equal(out.Reason, actionViewDetail.Reason)
	if out.Subject.AdminDefs_RepoRef != nil {
		assert.Equal(out.Subject.AdminDefs_RepoRef.Did, actionViewDetail.Subject.AdminDefs_RepoView.Did)
	} else if out.Subject.RepoStrongRef != nil {
		assert.Equal(out.Subject.RepoStrongRef.Uri, actionViewDetail.Subject.AdminDefs_RecordView.Uri)
		assert.Equal(out.Subject.RepoStrongRef.Cid, actionViewDetail.Subject.AdminDefs_RecordView.Cid)
	} else {
		t.Fatal("expected non-empty actionviewdetail.subject enum")
	}
	for i, blobCid := range out.SubjectBlobCids {
		assert.Equal(blobCid, actionViewDetail.SubjectBlobs[i].Cid)
	}
	assert.Equal(0, len(actionViewDetail.ResolvedReports))
	assert.Nil(actionViewDetail.Reversal)

	return out
}
