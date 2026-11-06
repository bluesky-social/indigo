package splitter

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atclient"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func TestRequestCrawlUpstreamError(t *testing.T) {
	assert := assert.New(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprintln(w, `{"error":"HostBanned","message":"host is banned"}`)
	}))
	defer upstream.Close()

	s, err := NewSplitter(SplitterConfig{UpstreamHost: upstream.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/xrpc/com.atproto.sync.requestCrawl", strings.NewReader(`{"hostname":"pds.example.com"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	assert.NoError(s.HandleComAtprotoSyncRequestCrawl(c))
	assert.Equal(http.StatusForbidden, rec.Code)

	var body atclient.ErrorBody
	assert.NoError(json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal("HostBanned", body.Name)
	assert.Equal("host is banned", body.Message)
}
