package server

import (
	"io"
	"net/http"
	"net/url"

	"github.com/bluesky-social/indigo/xrpc"
	"github.com/labstack/echo/v4"
)

// proxyToUpstream proxies a request to the upstream relay host.
func (s *Server) proxyToUpstream(c echo.Context) error {
	if s.cfg.ProxyHost == "" {
		return c.JSON(http.StatusServiceUnavailable, xrpc.XRPCError{
			ErrStr:  "ServiceUnavailable",
			Message: "no upstream host configured",
		})
	}

	u, err := url.Parse(s.cfg.ProxyHost)
	if err != nil {
		s.log.Error("failed to parse upstream host", "error", err)
		return c.JSON(http.StatusInternalServerError, xrpc.XRPCError{
			ErrStr:  "InternalServerError",
			Message: "invalid upstream host configuration",
		})
	}

	return s.proxyRequest(c, u.Host, u.Scheme)
}

// proxyToCollectionDir proxies a request to the collection directory service.
func (s *Server) proxyToCollectionDir(c echo.Context) error {
	if s.cfg.CollectionDirHost == "" {
		return c.JSON(http.StatusServiceUnavailable, xrpc.XRPCError{
			ErrStr:  "ServiceUnavailable",
			Message: "no collection directory host configured",
		})
	}

	u, err := url.Parse(s.cfg.CollectionDirHost)
	if err != nil {
		s.log.Error("failed to parse collection directory host", "error", err)
		return c.JSON(http.StatusInternalServerError, xrpc.XRPCError{
			ErrStr:  "InternalServerError",
			Message: "invalid collection directory host configuration",
		})
	}

	return s.proxyRequest(c, u.Host, u.Scheme)
}

// proxyRequest proxies an HTTP request to the specified host.
func (s *Server) proxyRequest(c echo.Context, hostname, scheme string) error {
	req := c.Request()
	resp := c.Response()

	// Build upstream URL
	upstreamURL := &url.URL{
		Scheme:   scheme,
		Host:     hostname,
		Path:     req.URL.Path,
		RawQuery: req.URL.RawQuery,
	}

	upstreamReq, err := http.NewRequestWithContext(req.Context(), req.Method, upstreamURL.String(), req.Body)
	if err != nil {
		s.log.Warn("proxy request failed", "error", err)
		return c.JSON(http.StatusBadRequest, xrpc.XRPCError{
			ErrStr:  "BadRequest",
			Message: "failed to create upstream request",
		})
	}

	// Copy subset of request headers
	for _, hdr := range []string{"Accept", "User-Agent", "Authorization", "Via", "Content-Type", "Content-Length"} {
		if val := req.Header.Get(hdr); val != "" {
			upstreamReq.Header.Set(hdr, val)
		}
	}

	upstreamResp, err := s.httpClient.Do(upstreamReq)
	if err != nil {
		s.log.Warn("upstream request failed", "error", err)
		return c.JSON(http.StatusBadGateway, xrpc.XRPCError{
			ErrStr:  "BadGateway",
			Message: "failed to reach upstream",
		})
	}
	defer func() { _ = upstreamResp.Body.Close() }()

	// Copy subset of response headers
	for _, hdr := range []string{"Content-Type", "Content-Length", "Location"} {
		if val := upstreamResp.Header.Get(hdr); val != "" {
			resp.Header().Set(hdr, val)
		}
	}
	resp.WriteHeader(upstreamResp.StatusCode)

	if _, err := io.Copy(resp, upstreamResp.Body); err != nil {
		s.log.Error("error copying proxy response body", "error", err)
	}

	return nil
}
