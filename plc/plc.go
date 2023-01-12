package plc

import (
	"context"

	"github.com/whyrusleeping/go-did"
	"github.com/whyrusleeping/gosky/key"
)

type PLCClient interface {
	GetDocument(ctx context.Context, didstr string) (*did.Document, error)
	CreateDID(ctx context.Context, sigkey *key.Key, recovery string, handle string, service string) (string, error)
}
