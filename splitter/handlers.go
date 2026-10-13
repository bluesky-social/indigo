package splitter

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/atclient"

	"github.com/labstack/echo/v4"
)

type HealthStatus struct {
	Service string `json:"service,const=rainbow"`
	Status  string `json:"status"`
	Message string `json:"msg,omitempty"`
}

func (s *Splitter) HandleHealthCheck(c echo.Context) error {
	return c.JSON(http.StatusOK, HealthStatus{Status: "ok"})
}

var homeMessage string = `
          _      _
 _ _ __ _(_)_ _ | |__  _____ __ __
| '_/ _' | | ' \| '_ \/ _ \ V  V /
|_| \__,_|_|_||_|_.__/\___/\_/\_/

This is an atproto [https://atproto.com] firehose fanout service, running the 'rainbow' codebase [https://github.com/bluesky-social/indigo]

The firehose WebSocket path is at:  /xrpc/com.atproto.sync.subscribeRepos
`

func (s *Splitter) HandleHomeMessage(c echo.Context) error {
	return c.String(http.StatusOK, homeMessage)
}

func (s *Splitter) HandleComAtprotoSyncRequestCrawl(c echo.Context) error {
	ctx := c.Request().Context()
	var body comatproto.SyncRequestCrawl_Input
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, atclient.ErrorBody{Name: "BadRequest", Message: fmt.Sprintf("invalid body: %s", err)})
	}
	if body.Hostname == "" {
		return c.JSON(http.StatusBadRequest, atclient.ErrorBody{Name: "BadRequest", Message: "must include a hostname"})
	}

	// first forward to the upstream
	client := &atclient.APIClient{
		Client: s.upstreamClient,
		Host:   s.conf.UpstreamHostHTTP(),
		Headers: map[string][]string{
			"User-Agent": []string{s.conf.UserAgent},
		},
	}

	err := comatproto.SyncRequestCrawl(ctx, client, &body)
	if err != nil {
		s.logger.Warn("failed to proxy requestCrawl", "targetHost", body.Hostname, "err", err)
		apiError, ok := err.(*atclient.APIError)
		if ok {
			// pass through the upstream error
			return c.JSON(apiError.StatusCode, atclient.ErrorBody{Name: apiError.Message, Message: apiError.Message})
		}
		return c.JSON(http.StatusBadGateway, atclient.ErrorBody{Name: "ProxyRequestFailed", Message: "could not connect to relay instance"})
	}

	// if that was successful, then forward on to the other upstreams (in goroutines)
	for _, nc := range s.nextCrawlers {
		// intentional local copy of loop variable
		crawler := nc.String()
		go func() {
			// new context to outlive original HTTP request
			ctx := context.Background()
			client := &atclient.APIClient{
				Client: s.peerClient,
				Host:   crawler,
				Headers: map[string][]string{
					"User-Agent": []string{s.conf.UserAgent},
				},
			}
			if err := comatproto.SyncRequestCrawl(ctx, client, &body); err != nil {
				s.logger.Warn("failed to forward requestCrawl", "crawler", crawler, "targetHost", body.Hostname, "err", err)
			} else {
				s.logger.Info("successfully forwarded requestCrawl", "crawler", crawler, "targetHost", body.Hostname)
			}
		}()
	}

	return c.JSON(http.StatusOK, map[string]any{"success": true})
}

// Proxies a request to the single upstream (relay)
func (s *Splitter) ProxyRequestUpstream(c echo.Context) error {
	u, err := url.Parse(s.conf.UpstreamHostHTTP())
	if err != nil {
		return err
	}
	return s.ProxyRequest(c, u.Host, u.Scheme)
}

// Proxies a request to the collectiondir
func (s *Splitter) ProxyRequestCollectionDir(c echo.Context) error {
	u, err := url.Parse(s.conf.CollectionDirHost)
	if err != nil {
		return err
	}
	return s.ProxyRequest(c, u.Host, u.Scheme)
}

func (s *Splitter) ProxyRequest(c echo.Context, hostname, scheme string) error {

	req := c.Request()
	respWriter := c.Response()

	u := req.URL
	u.Scheme = scheme
	u.Host = hostname
	upstreamReq, err := http.NewRequest(req.Method, u.String(), req.Body)
	if err != nil {
		s.logger.Warn("proxy request failed", "err", err)
		return c.JSON(http.StatusBadRequest, atclient.ErrorBody{Name: "BadRequest", Message: "failed to proxy to upstream relay"})
	}

	// copy subset of request headers
	for _, hdr := range []string{"Accept", "User-Agent", "Authorization", "Via", "Content-Type", "Content-Length"} {
		val := req.Header.Get(hdr)
		if val != "" {
			upstreamReq.Header.Set(hdr, val)
		}
	}

	upstreamResp, err := s.upstreamClient.Do(upstreamReq)
	if err != nil {
		return c.JSON(http.StatusBadRequest, atclient.ErrorBody{Name: "BadRequest", Message: "failed to proxy to upstream relay"})
	}
	defer upstreamResp.Body.Close()

	// copy a subset of response headers
	for _, hdr := range []string{"Content-Type", "Content-Length", "Location"} {
		val := upstreamResp.Header.Get(hdr)
		if val != "" {
			respWriter.Header().Set(hdr, val)
		}
	}
	respWriter.WriteHeader(upstreamResp.StatusCode)

	_, err = io.Copy(respWriter, upstreamResp.Body)
	if err != nil {
		s.logger.Error("error copying proxy body", "err", err)
	}

	return nil
}
