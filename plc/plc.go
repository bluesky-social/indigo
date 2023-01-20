package plc

import (
	"context"

	"github.com/bluesky-social/indigo/key"
	"github.com/whyrusleeping/go-did"
)

type PLCClient interface {
	GetDocument(ctx context.Context, didstr string) (*did.Document, error)
	CreateDID(ctx context.Context, sigkey *key.Key, recovery string, handle string, service string) (string, error)
}
