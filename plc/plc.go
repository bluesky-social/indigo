package plc

import (
	"context"

	"github.com/whyrusleeping/go-did"
)

type PLCClient interface {
	DidResolver
	CreateDID(ctx context.Context, sigkey *did.PrivKey, recovery string, handle string, service string) (string, error)
}

type DidResolver interface {
	GetDocument(ctx context.Context, didstr string) (*did.Document, error)
}
