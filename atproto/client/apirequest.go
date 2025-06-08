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
	// atproto API "Query" Lexicon method, which is HTTP GET. Not to be confused with the IETF draft "HTTP QUERY" method.
	MethodQuery = http.MethodGet

	// atproto API "Procedure" Lexicon method, which is HTTP POST.
	MethodProcedure = http.MethodPost
)

type APIRequest struct {
	// HTTP method as a string (eg "GET") (required)
	Method string

	// atproto API endpoint, as NSID (required)
	Endpoint syntax.NSID

	// Optional request body (may be nil). If this is provided, then 'Content-Type' header should be specified
	Body io.Reader

	// Optional function to return new reader for request body; used for retries. Strongly recommended if Body is defined. Body still needs to be defined, even if this function is provided.
	GetBody func() (io.ReadCloser, error)

	// Optional query parameters (field may be nil). These will be encoded as provided.
	QueryParams url.Values

	// Optional HTTP headers (field bay be nil). Only the first value will be included for each header key ("Set" behavior).
	Headers http.Header
}

// Initializes a new request struct. Initializes Headers and QueryParams so they can be manipulated immediately.
//
// If body is provided (it can be nil), will try to turn it in to the most retry-able form (and wrap as [io.ReadCloser]).
func NewAPIRequest(method string, endpoint syntax.NSID, body io.Reader) *APIRequest {
	req := APIRequest{
		Method:      method,
		Endpoint:    endpoint,
		Headers:     map[string][]string{},
		QueryParams: map[string][]string{},
	}

	// logic to turn "whatever io.Reader we are handed" in to something relatively re-tryable (using GetBody)
	if body != nil {
		// NOTE: http.NewRequestWithContext already handles GetBody() as well as ContentLength for specific types like bytes.Buffer and strings.Reader. We just want to add io.Seeker here, for things like files-on-disk.
		switch v := body.(type) {
		case io.Seeker:
			req.Body = io.NopCloser(body)
			req.GetBody = func() (io.ReadCloser, error) {
				v.Seek(0, 0)
				return io.NopCloser(body), nil
			}
		default:
			req.Body = body
		}
	}
	return &req
}

// Creates an [http.Request] for this API request.
//
// `host` parameter should be a URL prefix: schema, hostname, port (required)
//
// `clientHeaders`, if provided, is treated as client-level defaults. Only a single value is allowed per key ("Set" behavior), and will be clobbered by any request-level header values. (optional; may be nil)
func (r *APIRequest) HTTPRequest(ctx context.Context, host string, clientHeaders http.Header) (*http.Request, error) {
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
	if r.QueryParams != nil && len(r.QueryParams) > 0 {
		u.RawQuery = r.QueryParams.Encode()
	}
	httpReq, err := http.NewRequestWithContext(ctx, r.Method, u.String(), r.Body)
	if err != nil {
		return nil, err
	}

	if r.GetBody != nil {
		httpReq.GetBody = r.GetBody
	}

	// first set default headers...
	if clientHeaders != nil {
		for k := range clientHeaders {
			httpReq.Header.Set(k, clientHeaders.Get(k))
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
