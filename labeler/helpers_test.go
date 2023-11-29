package labeler

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
	label "github.com/bluesky-social/indigo/api/label"
)

// fetches report via getModerationReport, verifies match
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
	assert.Equal(out.Reason, reportViewDetail.Comment)
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

func testQueryLabels(t *testing.T, e *echo.Echo, lm *Server, params *url.Values) (*label.QueryLabels_Output, error) {

	req := httptest.NewRequest(http.MethodGet, "/xrpc/com.atproto.label.queryLabels?"+params.Encode(), nil)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	recorder := httptest.NewRecorder()
	c := e.NewContext(req, recorder)
	err := lm.HandleComAtprotoLabelQueryLabels(c)
	if err != nil {
		return nil, err
	}
	var out label.QueryLabels_Output
	if err := json.Unmarshal([]byte(recorder.Body.String()), &out); err != nil {
		t.Fatal(err)
	}
	return &out, nil
}
