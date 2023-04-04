package labeling

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
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
	assert.Equal(rt, *out.ReasonType)
	assert.Equal(reason, *out.Reason)
	// XXX: more fields
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

	// create a report
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
	reportOut := testCreateReport(t, e, lm, &report)

	_ = assert
	_ = reportOut
	// TODO: getReport helper (does single and multi, verifies equal, returns single)
	// TODO: getAction helper (does single and multi, verifies equal, returns single)

	// XXX: create action (including get, get plural, verifications)
	// XXX: get report (should have action included)
	// XXX: reverse action
	// XXX: get action (single and plural)
	// XXX: get report (should not have action included)
}
