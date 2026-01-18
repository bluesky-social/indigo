package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

func TestProxyToUpstream_NoUpstreamConfigured(t *testing.T) {
	s := &Server{
		cfg: Config{
			ProxyHost: "", // No upstream configured
		},
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/xrpc/com.atproto.sync.listRepos", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := s.proxyToUpstream(c)
	require.NoError(t, err)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Contains(t, rec.Body.String(), "no upstream host configured")
}

func TestProxyToUpstream_Success(t *testing.T) {
	// Create a mock upstream server
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/xrpc/com.atproto.sync.listRepos", r.URL.Path)
		require.Equal(t, "foo=bar", r.URL.RawQuery)
		require.Equal(t, "application/json", r.Header.Get("Accept"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"repos":[]}`))
	}))
	defer upstream.Close()

	s := &Server{
		cfg: Config{
			ProxyHost: upstream.URL,
		},
		httpClient: upstream.Client(),
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/xrpc/com.atproto.sync.listRepos?foo=bar", nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := s.proxyToUpstream(c)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	require.Equal(t, `{"repos":[]}`, rec.Body.String())
}

func TestProxyToUpstream_ForwardsHeaders(t *testing.T) {
	// Create a mock upstream server that echoes back headers
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers were forwarded
		require.Equal(t, "test-agent", r.Header.Get("User-Agent"))
		require.Equal(t, "Bearer token123", r.Header.Get("Authorization"))
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))

		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Location", "https://example.com/redirect")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	s := &Server{
		cfg: Config{
			ProxyHost: upstream.URL,
		},
		httpClient: upstream.Client(),
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/xrpc/com.atproto.sync.requestCrawl", strings.NewReader(`{"hostname":"example.com"}`))
	req.Header.Set("User-Agent", "test-agent")
	req.Header.Set("Authorization", "Bearer token123")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := s.proxyToUpstream(c)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "text/plain", rec.Header().Get("Content-Type"))
	require.Equal(t, "https://example.com/redirect", rec.Header().Get("Location"))
}

func TestProxyToUpstream_UpstreamError(t *testing.T) {
	// Create a mock upstream server that returns an error
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"NotFound","message":"repo not found"}`))
	}))
	defer upstream.Close()

	s := &Server{
		cfg: Config{
			ProxyHost: upstream.URL,
		},
		httpClient: upstream.Client(),
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/xrpc/com.atproto.sync.getRepo?did=did:plc:123", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := s.proxyToUpstream(c)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Contains(t, rec.Body.String(), "NotFound")
}

func TestProxyToUpstream_AdminEndpoint(t *testing.T) {
	// Create a mock upstream server for admin endpoints
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/admin/pds/list", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"pds":[]}`))
	}))
	defer upstream.Close()

	s := &Server{
		cfg: Config{
			ProxyHost: upstream.URL,
		},
		httpClient: upstream.Client(),
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/admin/pds/list", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := s.proxyToUpstream(c)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, `{"pds":[]}`, rec.Body.String())
}

func TestProxyToUpstream_PostWithBody(t *testing.T) {
	// Create a mock upstream server that echoes back the body
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, `{"hostname":"test.pds.com"}`, string(body))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer upstream.Close()

	s := &Server{
		cfg: Config{
			ProxyHost: upstream.URL,
		},
		httpClient: upstream.Client(),
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/xrpc/com.atproto.sync.requestCrawl", strings.NewReader(`{"hostname":"test.pds.com"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := s.proxyToUpstream(c)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, `{"success":true}`, rec.Body.String())
}
