package identity

import (
	"context"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// A fake identity directory , for use in tests
// TODO: should probably move this to a 'mockdirectory' sub-package?
type MockDirectory struct {
	Handles    map[syntax.Handle]syntax.DID
	Identities map[syntax.DID]Identity
}

var _ Directory = (*MockDirectory)(nil)

func NewMockDirectory() MockDirectory {
	return MockDirectory{
		Handles:    make(map[syntax.Handle]syntax.DID),
		Identities: make(map[syntax.DID]Identity),
	}
}

func (d *MockDirectory) Insert(ident Identity) {
	if !ident.Handle.IsInvalidHandle() {
		d.Handles[ident.Handle] = ident.DID
	}
	d.Identities[ident.DID] = ident
}

func (d *MockDirectory) LookupHandle(ctx context.Context, h syntax.Handle) (*Identity, error) {
	did, ok := d.Handles[h]
	if !ok {
		return nil, ErrHandleNotFound
	}
	ident, ok := d.Identities[did]
	if !ok {
		return nil, ErrDIDNotFound
	}
	return &ident, nil
}

func (d *MockDirectory) LookupDID(ctx context.Context, did syntax.DID) (*Identity, error) {
	ident, ok := d.Identities[did]
	if !ok {
		return nil, ErrDIDNotFound
	}
	return &ident, nil
}

func (d *MockDirectory) Lookup(ctx context.Context, a syntax.AtIdentifier) (*Identity, error) {
	handle, err := a.AsHandle()
	if nil == err { // if not an error, is a Handle
		return d.LookupHandle(ctx, handle)
	}
	did, err := a.AsDID()
	if nil == err { // if not an error, is a DID
		return d.LookupDID(ctx, did)
	}
	return nil, fmt.Errorf("at-identifier neither a Handle nor a DID")
}
