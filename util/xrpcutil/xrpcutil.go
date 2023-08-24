package xrpcutil

import (
	"context"
	"fmt"
	"net/http"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/pkg/errors"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// ClientOptions are the options for the XRPC Client
type ClientOptions struct {
	Host        string
	Identifier  string
	Password    string
	AutoRefresh bool
}

// DefaultClientOptions returns the default options for the XRPC Client
func DefaultClientOptions() *ClientOptions {
	return &ClientOptions{
		Host:        "https://bsky.social",
		AutoRefresh: true,
	}
}

// NewClient returns an XRPC client for the ATProto server
// with Authentication from the ATP_AUTH environment variable
func NewClient(ctx context.Context, opts *ClientOptions) (*xrpc.Client, error) {
	if opts == nil {
		opts = DefaultClientOptions()
	}

	// Create an instrumented transport for OTEL Tracing of HTTP Requests
	instrumentedTransport := otelhttp.NewTransport(&http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	})

	// Create the XRPC Client
	client := &xrpc.Client{
		Client: &http.Client{
			Transport: instrumentedTransport,
		},
		Host: opts.Host,
	}

	ses, err := comatproto.ServerCreateSession(ctx, client, &comatproto.ServerCreateSession_Input{
		Identifier: opts.Identifier,
		Password:   opts.Password,
	})
	if err != nil {
		e := errors.Wrap(err, "failed to create session")
		return nil, e
	}

	client.Auth = &xrpc.AuthInfo{
		Handle:     ses.Handle,
		Did:        ses.Did,
		RefreshJwt: ses.RefreshJwt,
		AccessJwt:  ses.AccessJwt,
	}

	if opts.AutoRefresh {
		go func() {
			t := time.NewTicker(10 * time.Minute)
			for {
				ctx := context.Background()
				select {
				case <-t.C:
					client.Mux.Lock()
					client.Auth.AccessJwt = client.Auth.RefreshJwt
					refreshedSession, err := comatproto.ServerRefreshSession(ctx, client)
					if err != nil {
						fmt.Printf("failed to refresh XRPC session: %s\n", err.Error())
						client.Mux.Unlock()
						continue
					}

					client.Auth = &xrpc.AuthInfo{
						AccessJwt:  refreshedSession.AccessJwt,
						RefreshJwt: refreshedSession.RefreshJwt,
						Did:        refreshedSession.Did,
						Handle:     refreshedSession.Handle,
					}
					client.Mux.Unlock()
				}
			}
		}()
	}

	return client, nil
}
