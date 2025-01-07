package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// NOTE: this is an interface so it can be wrapped/extended. eg, a variant with a bunch of retries, or caching, or whatever. maybe that is too complex and we should have simple struct type, more like the existing `indigo/xrpc` package? hrm.

type APIClient interface {
	// Full-power method for making atproto API requests.
	Do(ctx context.Context, req *APIRequest) (*http.Response, error)

	// High-level helper for simple JSON "Query" API calls.
	//
	// Does not work with all API endpoints. For more control, use the Do() method with APIRequest.
	Get(ctx context.Context, endpoint syntax.NSID, params map[string]string) (*json.RawMessage, error)

	// High-level helper for simple JSON-to-JSON "Procedure" API calls.
	//
	// Does not work with all API endpoints. For more control, use the Do() method with APIRequest.
	// TODO: what is the right type for body, to indicate it can be marshaled as JSON?
	Post(ctx context.Context, endpoint syntax.NSID, body any) (*json.RawMessage, error)

	// Returns the currently-authenticated account DID, or empty string if not available.
	AuthDID() syntax.DID
}

type APIRequest struct {
	HTTPVerb    string // TODO: type?
	Endpoint    syntax.NSID
	Body        io.Reader
	QueryParams map[string]string // TODO: better type for this?
	Headers     map[string]string
}

func (r *APIRequest) HTTPRequest(ctx context.Context, host string, headers map[string]string) (*http.Request, error) {
	// TODO: use 'url' to safely construct the request URL
	u := host + "/xrpc/" + r.Endpoint.String()
	// XXX: query params
	httpReq, err := http.NewRequestWithContext(ctx, r.HTTPVerb, u, r.Body)
	if err != nil {
		return nil, err
	}

	// first set default headers
	if headers != nil {
		for k, v := range headers {
			httpReq.Header.Set(k, v)
		}
	}

	// then request-specific take priority (overwrite)
	if r.Headers != nil {
		for k, v := range r.Headers {
			httpReq.Header.Set(k, v)
		}
	}

	return httpReq, nil
}
