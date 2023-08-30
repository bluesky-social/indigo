package identity

import (
	"context"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/hashicorp/golang-lru/v2/expirable"
)

type CacheDirectory struct {
	Inner         Directory
	ErrTTL        time.Duration
	handleCache   *expirable.LRU[syntax.Handle, HandleEntry]
	identityCache *expirable.LRU[syntax.DID, IdentityEntry]
}

type HandleEntry struct {
	Updated time.Time
	DID     syntax.DID
	Err     error
}

type IdentityEntry struct {
	Updated  time.Time
	Identity *Identity
	Err      error
}

var _ Directory = (*CacheDirectory)(nil)

// Capacity of zero means unlimited size. Similarly, ttl of zero means unlimited duration.
func NewCacheDirectory(inner Directory, capacity int, hitTTL, errTTL time.Duration) CacheDirectory {
	return CacheDirectory{
		ErrTTL:        errTTL,
		Inner:         inner,
		handleCache:   expirable.NewLRU[syntax.Handle, HandleEntry](capacity, nil, hitTTL),
		identityCache: expirable.NewLRU[syntax.DID, IdentityEntry](capacity, nil, hitTTL),
	}
}

func (d *CacheDirectory) IsHandleStale(e *HandleEntry) bool {
	if e.Err != nil && time.Since(e.Updated) > d.ErrTTL {
		return true
	}
	return false
}

func (d *CacheDirectory) IsIdentityStale(e *IdentityEntry) bool {
	if e.Err != nil && time.Since(e.Updated) > d.ErrTTL {
		return true
	}
	return false
}

func (d *CacheDirectory) updateHandle(ctx context.Context, h syntax.Handle) (*HandleEntry, error) {
	ident, err := d.Inner.LookupHandle(ctx, h)
	if err != nil {
		he := HandleEntry{
			Updated: time.Now(),
			DID:     "",
			Err:     err,
		}
		d.handleCache.Add(h, he)
		return &he, nil
	}

	entry := IdentityEntry{
		Updated:  time.Now(),
		Identity: ident,
		Err:      nil,
	}
	he := HandleEntry{
		Updated: time.Now(),
		DID:     ident.DID,
		Err:     nil,
	}

	d.identityCache.Add(ident.DID, entry)
	d.handleCache.Add(ident.Handle, he)
	return &he, nil
}

func (d *CacheDirectory) ResolveHandle(ctx context.Context, h syntax.Handle) (syntax.DID, error) {
	var err error
	var entry *HandleEntry
	maybeEntry, ok := d.handleCache.Get(h)

	if !ok {
		entry, err = d.updateHandle(ctx, h)
		if err != nil {
			return "", err
		}
	} else {
		entry = &maybeEntry
	}
	if d.IsHandleStale(entry) {
		entry, err = d.updateHandle(ctx, h)
		if err != nil {
			return "", err
		}
	}
	return entry.DID, entry.Err
}

func (d *CacheDirectory) updateDID(ctx context.Context, did syntax.DID) (*IdentityEntry, error) {
	ident, err := d.Inner.LookupDID(ctx, did)
	// persist the identity lookup error, instead of processing it immediately
	entry := IdentityEntry{
		Updated:  time.Now(),
		Identity: ident,
		Err:      err,
	}
	var he *HandleEntry
	// if *not* an error, then also update the handle cache
	if nil == err && !ident.Handle.IsInvalidHandle() {
		he = &HandleEntry{
			Updated: time.Now(),
			DID:     did,
			Err:     nil,
		}
	}

	d.identityCache.Add(did, entry)
	if he != nil {
		d.handleCache.Add(ident.Handle, *he)
	}
	return &entry, nil
}

func (d *CacheDirectory) LookupDID(ctx context.Context, did syntax.DID) (*Identity, error) {
	var err error
	var entry *IdentityEntry
	maybeEntry, ok := d.identityCache.Get(did)

	if !ok {
		entry, err = d.updateDID(ctx, did)
		if err != nil {
			return nil, err
		}
	} else {
		entry = &maybeEntry
	}
	if d.IsIdentityStale(entry) {
		entry, err = d.updateDID(ctx, did)
		if err != nil {
			return nil, err
		}
	}
	return entry.Identity, entry.Err
}

func (d *CacheDirectory) LookupHandle(ctx context.Context, h syntax.Handle) (*Identity, error) {
	did, err := d.ResolveHandle(ctx, h)
	if err != nil {
		return nil, err
	}
	ident, err := d.LookupDID(ctx, did)
	if err != nil {
		return nil, err
	}

	declared, err := ident.DeclaredHandle()
	if err != nil {
		return nil, err
	}
	if declared != h {
		return nil, fmt.Errorf("handle does not match that declared in DID document")
	}
	return ident, nil
}

func (d *CacheDirectory) Lookup(ctx context.Context, a syntax.AtIdentifier) (*Identity, error) {
	handle, err := a.AsHandle()
	if nil == err { // if not an error, is a handle
		return d.LookupHandle(ctx, handle)
	}
	did, err := a.AsDID()
	if nil == err { // if not an error, is a DID
		return d.LookupDID(ctx, did)
	}
	return nil, fmt.Errorf("at-identifier neither a Handle nor a DID")
}

func (d *CacheDirectory) Purge(ctx context.Context, a syntax.AtIdentifier) error {
	handle, err := a.AsHandle()
	if nil == err { // if not an error, is a handle
		d.handleCache.Remove(handle)
		return nil
	}
	did, err := a.AsDID()
	if nil == err { // if not an error, is a DID
		d.identityCache.Remove(did)
		return nil
	}
	return fmt.Errorf("at-identifier neither a Handle nor a DID")
}
