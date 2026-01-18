package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

func TestHandleHealth(t *testing.T) {
	s := &Server{}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/_health", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := s.handleHealth(c)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"service":"cask"`)
	require.Contains(t, rec.Body.String(), `"status":"ok"`)
}

func TestHandleHome(t *testing.T) {
	s := &Server{}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := s.handleHome(c)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "atproto")
	require.Contains(t, rec.Body.String(), "firehose")
	require.Contains(t, rec.Body.String(), "/xrpc/com.atproto.sync.subscribeRepos")
}

func TestRouter_CORSHeaders(t *testing.T) {
	s := &Server{
		cfg: Config{},
	}

	e := s.router()

	// Test preflight request
	req := httptest.NewRequest(http.MethodOptions, "/_health", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "GET")
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	require.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestRouter_ServerHeader(t *testing.T) {
	s := &Server{
		cfg: Config{
			UserAgent: "test-cask/1.0",
		},
	}

	e := s.router()

	req := httptest.NewRequest(http.MethodGet, "/_health", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "test-cask/1.0", rec.Header().Get("Server"))
}

func TestRouter_NoServerHeaderWhenNotConfigured(t *testing.T) {
	s := &Server{
		cfg: Config{
			UserAgent: "", // No user agent configured
		},
	}

	e := s.router()

	req := httptest.NewRequest(http.MethodGet, "/_health", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	// Server header should not be set by our middleware (Echo may set its own)
	require.NotEqual(t, "test-cask/1.0", rec.Header().Get("Server"))
}
