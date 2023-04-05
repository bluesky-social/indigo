package labeler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	label "github.com/bluesky-social/indigo/api/label"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func TestLabelMakerXRPCReportRepo(t *testing.T) {
	assert := assert.New(t)
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
	out := testCreateReport(t, e, lm, &report)
	assert.Equal(&rt, out.ReasonType)
	assert.Nil(out.Reason)
	assert.Equal(reportedDid, out.Subject.AdminDefs_RepoRef.Did)
}

func TestLabelMakerXRPCReportRepoBad(t *testing.T) {
	assert := assert.New(t)
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
			assert.Equal(row.statusCode, httpError.Code)
		} else {
			assert.Equal(row.statusCode, recorder.Code)
		}
	}
}

func TestLabelMakerXRPCReportRecord(t *testing.T) {
	assert := assert.New(t)
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
	out := testCreateReport(t, e, lm, &report)
	assert.Equal(report.ReasonType, out.ReasonType)
	assert.Equal(report.Reason, out.Reason)
	assert.NotNil(out.CreatedAt)
	assert.NotNil(out.ReportedBy)
	assert.Equal(report.Subject.AdminDefs_RepoRef, out.Subject.AdminDefs_RepoRef)
	assert.Equal(report.Subject.RepoStrongRef, out.Subject.RepoStrongRef)
}

func TestLabelMakerXRPCReportRecordBad(t *testing.T) {
	assert := assert.New(t)
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
			assert.Equal(row.statusCode, httpError.Code)
		} else {
			assert.Equal(row.statusCode, recorder.Code)
		}
	}
}

func TestLabelMakerXRPCReportAction(t *testing.T) {
	assert := assert.New(t)
	e := echo.New()
	lm := testLabelMaker(t)

	// create report
	reasonType := "spam"
	reportReason := "I just don't like it!"
	uri := "at://did:plc:123/com.example.record/bcd234"
	cid := "bafyreie5cvv4h45feadgeuwhbcutmh6t2ceseocckahdoe6uat64zmz454"
	report := comatproto.ModerationCreateReport_Input{
		Reason:     &reportReason,
		ReasonType: &reasonType,
		Subject: &comatproto.ModerationCreateReport_Input_Subject{
			RepoStrongRef: &comatproto.RepoStrongRef{
				//com.atproto.repo.strongRef
				Uri: uri,
				Cid: cid,
			},
		},
	}
	reportOut := testCreateReport(t, e, lm, &report)
	reportId := reportOut.Id

	// create action
	actionVerb := "acknowledge"
	actionDid := "did:plc:ADMIN"
	actionReason := "chaos reigns"
	action := comatproto.AdminTakeModerationAction_Input{
		Action:    actionVerb,
		CreatedBy: actionDid,
		Reason:    actionReason,
		Subject: &comatproto.AdminTakeModerationAction_Input_Subject{
			RepoStrongRef: &comatproto.RepoStrongRef{
				//com.atproto.repo.strongRef
				Uri: uri,
				Cid: cid,
			},
		},
		SubjectBlobCids: []string{
			"abc",
			"onetwothree",
		},
	}
	actionOut := testCreateAction(t, e, lm, &action)
	actionId := actionOut.Id

	// resolve report with action
	resolution := comatproto.AdminResolveModerationReports_Input{
		ActionId:  actionId,
		CreatedBy: actionDid,
		ReportIds: []int64{reportId},
	}
	resolutionJSON, err := json.Marshal(resolution)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/xrpc/com.atproto.report.resolveModerationReports", strings.NewReader(string(resolutionJSON)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	recorder := httptest.NewRecorder()
	c := e.NewContext(req, recorder)
	assert.NoError(lm.HandleComAtprotoAdminResolveModerationReports(c))
	var resolutionOut comatproto.AdminDefs_ActionView
	if err := json.Unmarshal([]byte(recorder.Body.String()), &resolutionOut); err != nil {
		t.Fatal(err)
	}
	fmt.Println(recorder.Body.String())
	assert.Equal(actionId, resolutionOut.Id)
	assert.Equal(1, len(resolutionOut.ResolvedReportIds))
	assert.Equal(reportId, resolutionOut.ResolvedReportIds[0])

	// get report (should have action included)
	reportOutDetail := testGetReport(t, e, lm, reportId)
	assert.Equal(reportId, reportOutDetail.Id)
	assert.Equal(1, len(reportOutDetail.ResolvedByActions))
	assert.Equal(actionId, reportOutDetail.ResolvedByActions[0].Id)

	// get action (should have report included)
	actionOutDetail := testGetAction(t, e, lm, actionId)
	assert.Equal(actionId, actionOutDetail.Id)
	assert.Equal(1, len(actionOutDetail.ResolvedReports))
	assert.Equal(reportId, actionOutDetail.ResolvedReports[0].Id)

	// reverse action
	reversalReason := "changed my mind"
	reversal := comatproto.AdminReverseModerationAction_Input{
		Id:        actionId,
		CreatedBy: actionDid,
		Reason:    reversalReason,
	}
	reversalJSON, err := json.Marshal(reversal)
	if err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodPost, "/xrpc/com.atproto.report.reverseModerationAction", strings.NewReader(string(reversalJSON)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	recorder = httptest.NewRecorder()
	c = e.NewContext(req, recorder)
	assert.NoError(lm.HandleComAtprotoAdminReverseModerationAction(c))
	var reversalOut comatproto.AdminDefs_ActionView
	if err := json.Unmarshal([]byte(recorder.Body.String()), &reversalOut); err != nil {
		t.Fatal(err)
	}
	assert.Equal(actionId, reversalOut.Id)
	assert.Equal(1, len(reversalOut.ResolvedReportIds))
	assert.Equal(reportId, reversalOut.ResolvedReportIds[0])
	assert.Equal(reversal.Reason, reversalOut.Reversal.Reason)
	assert.Equal(reversal.CreatedBy, reversalOut.Reversal.CreatedBy)
	assert.NotNil(reversalOut.Reversal.CreatedAt)

	// get report (should *not* have action included)
	reportOutDetail = testGetReport(t, e, lm, reportId)
	assert.Equal(reportId, reportOutDetail.Id)
	assert.Equal(0, len(reportOutDetail.ResolvedByActions))

	// get action (should still have report included)
	actionOutDetail = testGetAction(t, e, lm, actionId)
	assert.Equal(actionId, actionOutDetail.Id)
	assert.Equal(1, len(actionOutDetail.ResolvedReports))
	assert.Equal(reportId, actionOutDetail.ResolvedReports[0].Id)
	assert.Equal(reversalOut.Reversal, actionOutDetail.Reversal)
}

func TestLabelMakerXRPCLabelQuery(t *testing.T) {
	assert := assert.New(t)
	e := echo.New()
	lm := testLabelMaker(t)
	ctx := context.TODO()

	// simple query, no labels
	p1 := make(url.Values)
	p1.Set("uriPatterns", "*")
	out1, err := testQueryLabels(t, e, lm, &p1)
	assert.NoError(err)
	assert.Equal(0, len(out1.Labels))

	// create a label, then query
	l3 := label.Label{
		Uri: "at://did:plc:fake/com.example/abc234",
		Val: "example",
		Cts: "2023-03-15T22:16:18.408Z",
	}
	lm.CommitLabels(ctx, []*label.Label{&l3}, false)
	p3 := make(url.Values)
	p3.Set("uriPatterns", l3.Uri)
	out3, err := testQueryLabels(t, e, lm, &p3)
	assert.NoError(err)
	assert.Equal(1, len(out3.Labels))
	assert.Equal(&l3, out3.Labels[0])
}

func TestDidFromURI(t *testing.T) {
	assert := assert.New(t)
	cases := []struct {
		input    string
		expected string
	}{
		{input: "", expected: ""},
		{input: "at://did:plc:fake/com.example/abc234", expected: "did:plc:fake"},
		{input: "at://example.com/com.example/abc234", expected: ""},
		{input: "at://did:plc:fake", expected: "did:plc:fake"},
	}

	for _, tc := range cases {
		out := didFromURI(tc.input)
		assert.Equal(tc.expected, out)
	}
}
