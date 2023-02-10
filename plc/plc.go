package plc

import (
	"context"

	"github.com/whyrusleeping/go-did"
)

type PLCClient interface {
	GetDocument(ctx context.Context, didstr string) (*did.Document, error)
	CreateDID(ctx context.Context, sigkey *did.PrivKey, recovery string, handle string, service string) (string, error)
}
