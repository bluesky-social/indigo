package identity

import (
	"context"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

type BasicCatalog struct {
	PLCURL string
}

var _ Catalog = (*BasicCatalog)(nil)

func NewBasicCatalog(plcURL string) BasicCatalog {
	if plcURL == "" {
		plcURL = DefaultPLCURL
	}
	return BasicCatalog{
		PLCURL: plcURL,
	}
}

func (c *BasicCatalog) ResolveDID(ctx context.Context, did syntax.DID) (*DIDDocument, error) {
	switch did.Method() {
	case "web":
		return ResolveDIDWeb(ctx, did)
	case "plc":
		return ResolveDIDPLC(ctx, c.PLCURL, did)
	default:
		return nil, fmt.Errorf("DID method not supported: %s", did.Method())
	}
}

func (c *BasicCatalog) LookupHandle(ctx context.Context, h syntax.Handle) (*Identity, error) {
	did, err := ResolveHandle(ctx, h)
	if err != nil {
		return nil, err
	}
	doc, err := c.ResolveDID(ctx, did)
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

func (c *BasicCatalog) LookupDID(ctx context.Context, did syntax.DID) (*Identity, error) {
	doc, err := c.ResolveDID(ctx, did)
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

func (c *BasicCatalog) Lookup(ctx context.Context, a syntax.AtIdentifier) (*Identity, error) {
	handle, err := a.AsHandle()
	if nil == err { // if *not* an error
		return c.LookupHandle(ctx, handle)
	}
	did, err := a.AsDID()
	if nil == err { // if *not* an error
		return c.LookupDID(ctx, did)
	}
	return nil, fmt.Errorf("at-identifier neither a Handle nor a DID")
}
