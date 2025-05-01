package main

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"github.com/bluesky-social/indigo/cmd/relay/relay"

	"github.com/labstack/echo/v4"
)

// Forwards HTTP request on to sibling relay instances, if they are configured.
//
// This method expects to be run "in the background" as a goroutine, so it doesn't take a `context.Context`, and does not return an `error`. It logs both success and failure. The `echo.Context` request has presumably been read, so any body is passed separately. The `echo.Context` response is presumably being returned or already finalized concurrently.
func (s *Service) ForwardSiblingRequest(c echo.Context, body []byte) {

	if len(s.config.SiblingRelayHosts) == 0 {
		return
	}

	// if the request itself was already forwarded, or user-agent seems to be a relay, then don't forward on further (to prevent loops)
	req := c.Request()
	for _, via := range req.Header.Values("Via") {
		if strings.Contains(via, "atproto-relay") {
			s.logger.Info("not re-forwarding request to sibling relay", "header", "Via", "value", via)
			return
		}
	}
	for _, ua := range req.Header.Values("User-Agent") {
		if strings.Contains(ua, "atproto-relay") {
			s.logger.Info("not re-forwarding request to sibling relay", "header", "User-Agent", "value", ua)
			return
		}
	}

	for _, rawHost := range s.config.SiblingRelayHosts {
		hostname, noSSL, err := relay.ParseHostname(rawHost)
		if err != nil {
			s.logger.Error("invalid sibling hostname configured", "host", rawHost, "err", err)
			return
		}
		u := req.URL
		u.Host = hostname
		if noSSL {
			u.Scheme = "http"
		} else {
			u.Scheme = "https"
		}
		var b io.Reader
		if body != nil {
			b = bytes.NewBuffer(body)
		}
		upstreamReq, err := http.NewRequest(req.Method, u.String(), b)
		if err != nil {
			s.logger.Error("creating admin forward request failed", "method", req.Method, "url", u.String(), "err", err)
			continue
		}

		// copy some headers from inbound request
		for _, hdr := range []string{"Accept", "User-Agent", "Authorization", "Content-Type"} {
			val := req.Header.Get(hdr)
			if val != "" {
				upstreamReq.Header.Set(hdr, val)
			}
		}

		// add Via header (critical to prevent forwarding loops)
		upstreamReq.Header.Add("Via", req.Proto+" atproto-relay")

		upstreamResp, err := s.siblingClient.Do(upstreamReq)
		if err != nil {
			s.logger.Warn("forwarded admin HTTP request failed", "method", req.Method, "sibling", hostname, "url", u.String(), "err", err)
			continue
		}
		if !(upstreamResp.StatusCode >= 200 && upstreamResp.StatusCode < 300) {
			respBytes, _ := io.ReadAll(upstreamResp.Body)
			s.logger.Warn("forwarded admin HTTP request failed", "method", req.Method, "sibling", hostname, "url", u.String(), "statusCode", upstreamResp.StatusCode, "body", string(respBytes))
			continue
		}
		upstreamResp.Body.Close()
		s.logger.Info("successfully forwarded admin HTTP request", "method", req.Method, "url", u.String())
	}
}
