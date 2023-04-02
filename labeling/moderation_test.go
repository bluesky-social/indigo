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

func TestLabelMakerXRPCReportRepo(t *testing.T) {
	e := echo.New()
	lm := testLabelMaker(t)

	// create and read back a basic repo report
	rt := "spam"
	reportedDid := "did:plc:123"
	report := comatproto.ModerationCreateReport_Input{
		//Reason
		ReasonType: &rt,
		Subject: &comatproto.ModerationCreateReport_Input_Subject{
			AdminDefs_RepoRef: &comatproto.AdminDefs_RepoRef{
				Did: reportedDid,
			},
		},
	}
	reportJSON, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/xrpc/com.atproto.report.create", strings.NewReader(string(reportJSON)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	recorder := httptest.NewRecorder()
	c := e.NewContext(req, recorder)

	assert.NoError(t, lm.HandleComAtprotoReportCreate(c))
	// TODO: "Created" / 201
	assert.Equal(t, 200, recorder.Code)

	var out comatproto.ModerationCreateReport_Output
	if err := json.Unmarshal([]byte(recorder.Body.String()), &out); err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, report.ReasonType, out.ReasonType)
	assert.Equal(t, report.Subject.AdminDefs_RepoRef, out.Subject.AdminDefs_RepoRef)
	reportId := out.Id

	// read it back
	params := make(url.Values)
	params.Set("id", strconv.Itoa(int(reportId)))
	req = httptest.NewRequest(http.MethodGet, "/xrpc/com.atproto.admin.getModerationReport?"+params.Encode(), nil)
	recorder = httptest.NewRecorder()
	c = e.NewContext(req, recorder)
	assert.NoError(t, lm.HandleComAtprotoAdminGetModerationReport(c))
	assert.Equal(t, 200, recorder.Code)
	var vd comatproto.AdminDefs_ReportViewDetail
	if err := json.Unmarshal([]byte(recorder.Body.String()), &vd); err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, vd.Id, reportId, vd.Id)
	assert.Equal(t, vd.ReasonType, report.ReasonType)
	assert.Nil(t, vd.Reason)
	assert.Equal(t, vd.Subject.AdminDefs_RepoView.Did, reportedDid)
	assert.Nil(t, vd.Subject.AdminDefs_RecordView)
	// TODO: additional AdminDefs_RecordView fields

	// read back via get multi
	req = httptest.NewRequest(http.MethodGet, "/xrpc/com.atproto.admin.getModerationReports", nil)
	recorder = httptest.NewRecorder()
	c = e.NewContext(req, recorder)
	assert.NoError(t, lm.HandleComAtprotoAdminGetModerationReports(c))
	assert.Equal(t, 200, recorder.Code)
	var reportsOut comatproto.AdminGetModerationReports_Output
	if err := json.Unmarshal([]byte(recorder.Body.String()), &reportsOut); err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, len(reportsOut.Reports), 1)
	assert.Equal(t, reportsOut.Reports[0].Id, reportId)

}

func TestLabelMakerXRPCReportRepoBad(t *testing.T) {
	e := echo.New()
	lm := testLabelMaker(t)

	table := []struct {
		rType      string
		rDid       string
		statusCode int
	}{
		{"spam", "did:plc:123", 200},
		{"", "did:plc:123", 400},
		{"spam", "", 400},
	}

	for _, row := range table {

		report := comatproto.ModerationCreateReport_Input{
			//Reason
			ReasonType: &row.rType,
			Subject: &comatproto.ModerationCreateReport_Input_Subject{
				AdminDefs_RepoRef: &comatproto.AdminDefs_RepoRef{
					Did: row.rDid,
				},
			},
		}
		reportJSON, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/xrpc/com.atproto.report.create", strings.NewReader(string(reportJSON)))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		recorder := httptest.NewRecorder()
		c := e.NewContext(req, recorder)
		err = lm.HandleComAtprotoReportCreate(c)
		if err != nil {
			httpError, _ := err.(*echo.HTTPError)
			assert.Equal(t, row.statusCode, httpError.Code)
		} else {
			assert.Equal(t, row.statusCode, recorder.Code)
		}
	}
}

func TestLabelMakerXRPCReportRecord(t *testing.T) {
	e := echo.New()
	lm := testLabelMaker(t)
	// create a second report, on a record
	rt := "spam"
	reason := "I just don't like it!"
	uri := "at://did:plc:123/com.example.record/bcd234"
	cid := "bafyreie5cvv4h45feadgeuwhbcutmh6t2ceseocckahdoe6uat64zmz454"
	report := comatproto.ModerationCreateReport_Input{
		Reason:     &reason,
		ReasonType: &rt,
		Subject: &comatproto.ModerationCreateReport_Input_Subject{
			RepoStrongRef: &comatproto.RepoStrongRef{
				//com.atproto.repo.strongRef
				Uri: uri,
				Cid: cid,
			},
		},
	}
	reportJSON, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/xrpc/com.atproto.report.create", strings.NewReader(string(reportJSON)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	recorder := httptest.NewRecorder()
	c := e.NewContext(req, recorder)

	assert.NoError(t, lm.HandleComAtprotoReportCreate(c))
	// TODO: "Created" / 201
	assert.Equal(t, 200, recorder.Code)

	var out comatproto.ModerationCreateReport_Output
	if err := json.Unmarshal([]byte(recorder.Body.String()), &out); err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, report.ReasonType, out.ReasonType)
	assert.Equal(t, report.Subject.AdminDefs_RepoRef, out.Subject.AdminDefs_RepoRef)
	reportId := out.Id

	// read it back
	params := make(url.Values)
	params.Set("id", strconv.Itoa(int(reportId)))
	req = httptest.NewRequest(http.MethodGet, "/xrpc/com.atproto.admin.getModerationReport?"+params.Encode(), nil)
	recorder = httptest.NewRecorder()
	c = e.NewContext(req, recorder)
	assert.NoError(t, lm.HandleComAtprotoAdminGetModerationReport(c))
	assert.Equal(t, 200, recorder.Code)
	var vd comatproto.AdminDefs_ReportViewDetail
	if err := json.Unmarshal([]byte(recorder.Body.String()), &vd); err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, reportId, vd.Id)
	assert.Equal(t, *report.Reason, reason)
	assert.Equal(t, report.ReasonType, vd.ReasonType)
}

func TestLabelMakerXRPCReportRecordBad(t *testing.T) {
	e := echo.New()
	lm := testLabelMaker(t)

	uriStr := "at://did:plc:123/com.example.record/bcd234"
	cidStr := "bafyreie5cvv4h45feadgeuwhbcutmh6t2ceseocckahdoe6uat64zmz454"
	emptyStr := ""
	table := []struct {
		rType      string
		rUri       string
		rCid       string
		statusCode int
	}{
		{"spam", uriStr, cidStr, 200},
		{"", uriStr, cidStr, 400},
		{"spam", "", cidStr, 400},
		{"spam", uriStr, emptyStr, 400},
	}

	for _, row := range table {

		report := comatproto.ModerationCreateReport_Input{
			ReasonType: &row.rType,
			Subject: &comatproto.ModerationCreateReport_Input_Subject{
				RepoStrongRef: &comatproto.RepoStrongRef{
					//com.atproto.repo.strongRef
					Uri: row.rUri,
					Cid: row.rCid,
				},
			},
		}
		reportJSON, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/xrpc/com.atproto.report.create", strings.NewReader(string(reportJSON)))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		recorder := httptest.NewRecorder()
		c := e.NewContext(req, recorder)
		err = lm.HandleComAtprotoReportCreate(c)
		if err != nil {
			httpError, _ := err.(*echo.HTTPError)
			assert.Equal(t, row.statusCode, httpError.Code)
		} else {
			assert.Equal(t, row.statusCode, recorder.Code)
		}
	}
}

func TestLabelMakerXRPCReportAction(t *testing.T) {
	//e := echo.New()
	//lm := testLabelMaker(t)

	// XXX: create report
	// XXX: action report
	// XXX: get action
	// XXX: get actions (plural)
	// XXX: get report (should have action included)
	// XXX: reverse action
	// XXX: get action
	// XXX: get actions (plural)
	// XXX: get report (should not have action included)
}
