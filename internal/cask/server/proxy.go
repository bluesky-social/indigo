package server

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/labstack/echo/v4"
)

// proxyToUpstream proxies a request to the upstream relay host.
func (s *Server) proxyToUpstream(c echo.Context) error {
	if s.proxyHostURL == nil {
		return c.JSON(http.StatusServiceUnavailable, xrpc.XRPCError{
			ErrStr:  "ServiceUnavailable",
			Message: "no upstream host configured",
		})
	}

	return s.proxyRequest(c, s.proxyHostURL.Host, s.proxyHostURL.Scheme)
}

// proxyToCollectionDir proxies a request to the collection directory service.
func (s *Server) proxyToCollectionDir(c echo.Context) error {
	if s.collectionDirHostURL == nil {
		return c.JSON(http.StatusServiceUnavailable, xrpc.XRPCError{
			ErrStr:  "ServiceUnavailable",
			Message: "no collection directory host configured",
		})
	}

	return s.proxyRequest(c, s.collectionDirHostURL.Host, s.collectionDirHostURL.Scheme)
}

// handleRequestCrawl handles com.atproto.sync.requestCrawl by first forwarding to upstream,
// then async-forwarding to all configured next-crawlers.
func (s *Server) handleRequestCrawl(c echo.Context) error {
	// Read and parse the request body
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return c.JSON(http.StatusBadRequest, xrpc.XRPCError{
			ErrStr:  "BadRequest",
			Message: fmt.Sprintf("failed to read request body: %s", err),
		})
	}

	// Reset body for binding since we already read it
	c.Request().Body = io.NopCloser(bytes.NewReader(body))

	var input comatproto.SyncRequestCrawl_Input
	if err := c.Bind(&input); err != nil {
		return c.JSON(http.StatusBadRequest, xrpc.XRPCError{
			ErrStr:  "BadRequest",
			Message: fmt.Sprintf("invalid body: %s", err),
		})
	}
	if input.Hostname == "" {
		return c.JSON(http.StatusBadRequest, xrpc.XRPCError{
			ErrStr:  "BadRequest",
			Message: "must include a hostname",
		})
	}

	// First forward to upstream
	if s.cfg.ProxyHost != "" {
		xrpcc := xrpc.Client{
			Client: s.httpClient,
			Host:   s.cfg.ProxyHost,
		}
		if s.cfg.UserAgent != "" {
			xrpcc.UserAgent = &s.cfg.UserAgent
		}

		if err := comatproto.SyncRequestCrawl(c.Request().Context(), &xrpcc, &input); err != nil {
			if httpErr, ok := err.(*xrpc.Error); ok {
				return c.JSON(httpErr.StatusCode, xrpc.XRPCError{
					ErrStr:  "UpstreamError",
					Message: fmt.Sprintf("%s", httpErr.Wrapped),
				})
			}
			return c.JSON(http.StatusInternalServerError, xrpc.XRPCError{
				ErrStr:  "ProxyRequestFailed",
				Message: fmt.Sprintf("failed forwarding request: %s", err),
			})
		}
	}

	// Forward to all next-crawlers asynchronously using the robust peer client
	for _, crawler := range s.nextCrawlerURLs {
		crawlerURL := crawler
		go func() {
			ctx := context.Background()
			xrpcc := xrpc.Client{
				Client: s.peerClient,
				Host:   crawlerURL,
			}
			if err := comatproto.SyncRequestCrawl(ctx, &xrpcc, &input); err != nil {
				s.log.Warn("failed to forward requestCrawl", "crawler", crawlerURL, "targetHost", input.Hostname, "err", err)
			} else {
				s.log.Debug("forwarded requestCrawl", "crawler", crawlerURL, "targetHost", input.Hostname)
			}
		}()
	}

	return c.JSON(http.StatusOK, map[string]any{"success": true})
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
