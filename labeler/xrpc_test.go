package labeler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	comatproto "github.com/bluesky-social/indigo/api/atproto"

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
	l3 := comatproto.LabelDefs_Label{
		Uri: "at://did:plc:fake/com.example/abc234",
		Val: "example",
		Cts: "2023-03-15T22:16:18.408Z",
	}
	lm.CommitLabels(ctx, []*comatproto.LabelDefs_Label{&l3}, false)
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
