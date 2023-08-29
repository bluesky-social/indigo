package identity

import (
	"context"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// A fake identity catalog, for use in tests
// TODO: should probably move this to a 'mockcatalog' sub-package?
type MockCatalog struct {
	Handles    map[syntax.Handle]syntax.DID
	Identities map[syntax.DID]Identity
}

var _ Catalog = (*MockCatalog)(nil)

func NewMockCatalog() MockCatalog {
	return MockCatalog{
		Handles:    make(map[syntax.Handle]syntax.DID),
		Identities: make(map[syntax.DID]Identity),
	}
}

func (c *MockCatalog) Insert(ident Identity) {
	if !ident.Handle.IsInvalidHandle() {
		c.Handles[ident.Handle] = ident.DID
	}
	c.Identities[ident.DID] = ident
}

func (c *MockCatalog) LookupHandle(ctx context.Context, h syntax.Handle) (*Identity, error) {
	did, ok := c.Handles[h]
	if !ok {
		return nil, ErrHandleNotFound
	}
	ident, ok := c.Identities[did]
	if !ok {
		return nil, ErrDIDNotFound
	}
	return &ident, nil
}

func (c *MockCatalog) LookupDID(ctx context.Context, did syntax.DID) (*Identity, error) {
	ident, ok := c.Identities[did]
	if !ok {
		return nil, ErrDIDNotFound
	}
	return &ident, nil
}

func (c *MockCatalog) Lookup(ctx context.Context, a syntax.AtIdentifier) (*Identity, error) {
	handle, err := a.AsHandle()
	if nil == err { // if not an error, is a Handle
		return c.LookupHandle(ctx, handle)
	}
	did, err := a.AsDID()
	if nil == err { // if not an error, is a DID
		return c.LookupDID(ctx, did)
	}
	return nil, fmt.Errorf("at-identifier neither a Handle nor a DID")
}
