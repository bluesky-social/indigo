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

func TestLabelMakerXRPCReport(t *testing.T) {
	e := echo.New()
	lm := testLabelMaker(t)

	// create and read back a basic repo report
	rt := "spam"
	report := comatproto.ReportCreate_Input{
		//Reason
		ReasonType: &rt,
		Subject: &comatproto.ReportCreate_Input_Subject{
			RepoRepoRef: &comatproto.RepoRepoRef{
				//com.atproto.repo.repoRef
				Did: "did:plc:123",
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

	var out comatproto.ReportCreate_Output
	if err := json.Unmarshal([]byte(recorder.Body.String()), &out); err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, report.ReasonType, out.ReasonType)
	assert.Equal(t, report.Subject.RepoRepoRef, out.Subject.RepoRepoRef)

	// read it back
	params := make(url.Values)
	params.Set("id", strconv.Itoa(int(out.Id)))
	req = httptest.NewRequest(http.MethodGet, "/xrpc/com.atproto.admin.getModerationReport?"+params.Encode(), nil)
	recorder = httptest.NewRecorder()
	c = e.NewContext(req, recorder)
	assert.NoError(t, lm.HandleComAtprotoAdminGetModerationReport(c))
	assert.Equal(t, 200, recorder.Code)

	var view comatproto.AdminModerationReport_ViewDetail
	if err := json.Unmarshal([]byte(recorder.Body.String()), &view); err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, report.ReasonType, view.ReasonType)
}
