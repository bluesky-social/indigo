package plc

import (
	"context"

	didres "github.com/gander-social/gander-indigo-sovereign/did"
	"github.com/whyrusleeping/go-did"
)

type PLCClient interface {
	didres.Resolver
	CreateDID(ctx context.Context, sigkey *did.PrivKey, recovery string, handle string, service string) (string, error)
	UpdateUserHandle(ctx context.Context, didstr string, nhandle string) error
}
