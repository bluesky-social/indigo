package util

import (
	"context"
)

type XRPCRequestType int

const (
	Query = XRPCRequestType(iota)
	Procedure
)

// API client interface used in lexgen.
type LexClient interface {
	LexDo(ctx context.Context, kind XRPCRequestType, inpenc string, method string, params map[string]any, bodyobj any, out any) error
}
