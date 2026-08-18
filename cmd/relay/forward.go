package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
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
			upstreamResp.Body.Close()
			s.logger.Warn("forwarded admin HTTP request failed", "method", req.Method, "sibling", hostname, "url", u.String(), "statusCode", upstreamResp.StatusCode, "body", string(respBytes))
			continue
		}
		upstreamResp.Body.Close()
		s.logger.Info("successfully forwarded admin HTTP request", "method", req.Method, "url", u.String())
	}
}

func (s *Service) ForwardSiblingAccountLimitAlertSent(ctx context.Context, state AccountLimitAlertSentState) {
	if len(s.config.SiblingRelayHosts) == 0 {
		return
	}
	if len(s.config.AdminPasswords) == 0 {
		s.logger.Warn("not forwarding account limit alert sent state because no admin password is configured")
		return
	}

	body, err := json.Marshal(state)
	if err != nil {
		s.logger.Error("marshaling account limit alert sent state failed", "err", err)
		return
	}
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:"+s.config.AdminPasswords[0]))

	for _, rawHost := range s.config.SiblingRelayHosts {
		hostname, noSSL, err := relay.ParseHostname(rawHost)
		if err != nil {
			s.logger.Error("invalid sibling hostname configured", "host", rawHost, "err", err)
			return
		}

		scheme := "https"
		if noSSL {
			scheme = "http"
		}
		u := fmt.Sprintf("%s://%s/admin/alerts/accountLimitSent", scheme, hostname)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
		if err != nil {
			s.logger.Error("creating alert state forward request failed", "sibling", hostname, "err", err)
			continue
		}
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", s.relay.Config.UserAgent)
		req.Header.Add("Via", "HTTP/1.1 atproto-relay")

		resp, err := s.siblingClient.Do(req)
		if err != nil {
			s.logger.Warn("forwarded account limit alert state failed", "sibling", hostname, "kind", state.Kind, "host", state.Hostname, "err", err)
			continue
		}
		if !(resp.StatusCode >= 200 && resp.StatusCode < 300) {
			respBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			s.logger.Warn("forwarded account limit alert state failed", "sibling", hostname, "kind", state.Kind, "host", state.Hostname, "statusCode", resp.StatusCode, "body", string(respBytes))
			continue
		}
		resp.Body.Close()
		s.logger.Info("successfully forwarded account limit alert state", "sibling", hostname, "kind", state.Kind, "host", state.Hostname)
	}
}
