package identity

import (
	"context"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

type BaseDirectory struct {
	PLCURL string
}

var _ Directory = (*BaseDirectory)(nil)

func NewBaseDirectory(plcURL string) BaseDirectory {
	if plcURL == "" {
		plcURL = DefaultPLCURL
	}
	return BaseDirectory{
		PLCURL: plcURL,
	}
}

func (d *BaseDirectory) LookupHandle(ctx context.Context, h syntax.Handle) (*Identity, error) {
	did, err := d.ResolveHandle(ctx, h)
	if err != nil {
		return nil, err
	}
	doc, err := d.ResolveDID(ctx, did)
	if err != nil {
		return nil, err
	}
	ident := ParseIdentity(doc)
	declared, err := ident.DeclaredHandle()
	if err != nil {
		return nil, err
	}
	if declared != h {
		return nil, fmt.Errorf("handle does not match that declared in DID document")
	}
	ident.Handle = declared

	// optimistic caching of public key
	pk, err := ident.PublicKey()
	if nil == err {
		ident.ParsedPublicKey = pk
	}
	return &ident, nil
}

func (d *BaseDirectory) LookupDID(ctx context.Context, did syntax.DID) (*Identity, error) {
	doc, err := d.ResolveDID(ctx, did)
	if err != nil {
		return nil, err
	}
	ident := ParseIdentity(doc)
	declared, err := ident.DeclaredHandle()
	if err != nil {
		return nil, err
	}
	resolvedDID, err := d.ResolveHandle(ctx, declared)
	if err != nil {
		return nil, err
	}
	if resolvedDID == did {
		ident.Handle = declared
	}

	// optimistic caching of public key
	pk, err := ident.PublicKey()
	if nil == err {
		ident.ParsedPublicKey = pk
	}
	return &ident, nil
}

func (d *BaseDirectory) Lookup(ctx context.Context, a syntax.AtIdentifier) (*Identity, error) {
	handle, err := a.AsHandle()
	if nil == err { // if *not* an error
		return d.LookupHandle(ctx, handle)
	}
	did, err := a.AsDID()
	if nil == err { // if *not* an error
		return d.LookupDID(ctx, did)
	}
	return nil, fmt.Errorf("at-identifier neither a Handle nor a DID")
}
