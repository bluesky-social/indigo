package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

var (
	// atproto API "Query" Lexicon method, which is HTTP GET. Not to be confused with proposed "HTTP QUERY" method.
	MethodQuery = http.MethodGet

	// atproto API "Procedure" Lexicon method, which is HTTP POST.
	MethodProcedure = http.MethodPost
)

type APIRequest struct {
	// HTTP method, eg "GET" (required)
	Method string

	// atproto API endpoint, as NSID (required)
	Endpoint syntax.NSID

	// optional request body. if this is provided, then 'Content-Type' header should be specified
	Body io.Reader

	// XXX:
	//GetBody func() (io.ReadCloser, error)

	// optional query parameters. These will be encoded as provided.
	QueryParams url.Values

	// optional HTTP headers. Only the first value will be included for each header key ("Set" behavior).
	Headers http.Header
}

// Turns the API request in to an `http.Request`.
//
// `host` parameter should be a URL prefix: schema, hostname, port.
// `headers` parameters are treated as client-level defaults. Only a single value is allowed per key ("Set" behavior), and will be clobbered by any request-level header values.
func (r *APIRequest) HTTPRequest(ctx context.Context, host string, headers http.Header) (*http.Request, error) {
	u, err := url.Parse(host)
	if err != nil {
		return nil, err
	}
	if u.Host == "" {
		return nil, fmt.Errorf("empty hostname in host URL")
	}
	if u.Scheme == "" {
		return nil, fmt.Errorf("empty scheme in host URL")
	}
	if r.Endpoint == "" {
		return nil, fmt.Errorf("empty request endpoint")
	}
	u.Path = "/xrpc/" + r.Endpoint.String()
	u.RawQuery = ""
	if r.QueryParams != nil {
		u.RawQuery = r.QueryParams.Encode()
	}
	httpReq, err := http.NewRequestWithContext(ctx, r.Method, u.String(), r.Body)
	if err != nil {
		return nil, err
	}

	// first set default headers...
	if headers != nil {
		for k := range headers {
			httpReq.Header.Set(k, headers.Get(k))
		}
	}

	// ... then request-specific take priority (overwrite)
	if r.Headers != nil {
		for k := range r.Headers {
			httpReq.Header.Set(k, r.Headers.Get(k))
		}
	}

	return httpReq, nil
}
