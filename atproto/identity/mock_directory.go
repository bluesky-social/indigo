package identity

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// A fake identity directory, for use in tests
type MockDirectory struct {
	mu         *sync.RWMutex
	Handles    map[syntax.Handle]syntax.DID
	Identities map[syntax.DID]Identity
}

var _ Directory = (*MockDirectory)(nil)
var _ Resolver = (*MockDirectory)(nil)

func NewMockDirectory() MockDirectory {
	return MockDirectory{
		mu:         &sync.RWMutex{},
		Handles:    make(map[syntax.Handle]syntax.DID),
		Identities: make(map[syntax.DID]Identity),
	}
}

func (d *MockDirectory) Insert(ident Identity) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !ident.Handle.IsInvalidHandle() {
		d.Handles[ident.Handle.Normalize()] = ident.DID
	}
	d.Identities[ident.DID] = ident
}

func (d *MockDirectory) LookupHandle(ctx context.Context, h syntax.Handle) (*Identity, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	h = h.Normalize()
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
	d.mu.RLock()
	defer d.mu.RUnlock()

	ident, ok := d.Identities[did]
	if !ok {
		return nil, ErrDIDNotFound
	}
	return &ident, nil
}

func (d *MockDirectory) Lookup(ctx context.Context, a syntax.AtIdentifier) (*Identity, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

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

func (d *MockDirectory) ResolveHandle(ctx context.Context, h syntax.Handle) (syntax.DID, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	h = h.Normalize()
	did, ok := d.Handles[h]
	if !ok {
		return "", ErrHandleNotFound
	}
	return did, nil
}

func (d *MockDirectory) ResolveDID(ctx context.Context, did syntax.DID) (*DIDDocument, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	ident, ok := d.Identities[did]
	if !ok {
		return nil, ErrDIDNotFound
	}
	doc := ident.DIDDocument()
	return &doc, nil
}

func (d *MockDirectory) ResolveDIDRaw(ctx context.Context, did syntax.DID) (json.RawMessage, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	ident, ok := d.Identities[did]
	if !ok {
		return nil, ErrDIDNotFound
	}
	doc := ident.DIDDocument()
	return json.Marshal(doc)
}

func (d *MockDirectory) Purge(ctx context.Context, a syntax.AtIdentifier) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	return nil
}
