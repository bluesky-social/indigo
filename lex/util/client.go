package util

import (
	"context"
	"net/http"
)

const (
	Query     = http.MethodGet
	Procedure = http.MethodPost
)

// API client interface used in lexgen.
//
// 'method' is the HTTP method type. 'inputEncoding' is the Content-Type for bodyData in Procedure calls. 'params' are query parameters. 'bodyData' should be either 'nil', an [io.Reader], or a type which can be marshalled to JSON. 'out' is optional; if not nil it should be a pointer to a type which can be un-Marshaled as JSON, for the response body.
type LexClient interface {
	LexDo(ctx context.Context, method string, inputEncoding string, endpoint string, params map[string]any, bodyData any, out any) error
}
