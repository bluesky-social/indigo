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
type LexClient interface {
	LexDo(ctx context.Context, kind string, inpenc string, method string, params map[string]any, bodyobj any, out any) error
}
