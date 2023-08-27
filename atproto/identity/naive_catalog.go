package identity

import (
	"context"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

type NaiveCatalog struct {
	// TODO: actually wire through use of PLC host
	PLCHost string
}

var _ Catalog = (*NaiveCatalog)(nil)

func NewNaiveCatalog(plcHost string) NaiveCatalog {
	return NaiveCatalog{
		PLCHost: plcHost,
	}
}

func (c *NaiveCatalog) LookupHandle(ctx context.Context, h syntax.Handle) (*Identity, error) {
	did, err := ResolveHandle(ctx, h)
	if err != nil {
		return nil, err
	}
	doc, err := ResolveDID(ctx, did)
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

func (c *NaiveCatalog) LookupDID(ctx context.Context, did syntax.DID) (*Identity, error) {
	doc, err := ResolveDID(ctx, did)
	if err != nil {
		return nil, err
	}
	ident := ParseIdentity(doc)
	declared, err := ident.DeclaredHandle()
	if err != nil {
		return nil, err
	}
	resolvedDID, err := ResolveHandle(ctx, declared)
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

func (c *NaiveCatalog) Lookup(ctx context.Context, a syntax.AtIdentifier) (*Identity, error) {
	handle, err := a.AsHandle()
	if err == nil {
		return c.LookupHandle(ctx, handle)
	}
	did, err := a.AsDID()
	if err == nil {
		return c.LookupDID(ctx, did)
	}
	return nil, fmt.Errorf("at-identifier neither a Handle nor a DID")
}
