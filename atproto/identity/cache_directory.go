package identity

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

type CacheDirectory struct {
	HitTTL        time.Duration
	ErrTTL        time.Duration
	Inner         Directory
	mutex         sync.RWMutex
	handleCache   map[syntax.Handle]HandleEntry
	identityCache map[syntax.DID]IdentityEntry
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

func NewCacheDirectory(inner Directory) CacheDirectory {
	// NOTE: these are kind of arbitrary default values...
	hitTTL := time.Duration(1e9 * 60 * 60) // 1 hour
	errTTL := time.Duration(1e9 * 60 * 2)  // 2 minutes
	return CacheDirectory{
		HitTTL:        hitTTL,
		ErrTTL:        errTTL,
		Inner:         inner,
		handleCache:   make(map[syntax.Handle]HandleEntry, 10),
		identityCache: make(map[syntax.DID]IdentityEntry, 10),
	}
}

func (d *CacheDirectory) IsHandleStale(e *HandleEntry) bool {
	if nil == e.Err && time.Since(e.Updated) > d.HitTTL {
		return true
	}
	if e.Err != nil && time.Since(e.Updated) > d.ErrTTL {
		return true
	}
	return false
}

func (d *CacheDirectory) IsIdentityStale(e *IdentityEntry) bool {
	if nil == e.Err && time.Since(e.Updated) > d.HitTTL {
		return true
	}
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
		d.mutex.Lock()
		d.handleCache[h] = he
		d.mutex.Unlock()
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

	d.mutex.Lock()
	d.identityCache[ident.DID] = entry
	d.handleCache[ident.Handle] = he
	d.mutex.Unlock()
	return &he, nil
}

func (d *CacheDirectory) ResolveHandle(ctx context.Context, h syntax.Handle) (syntax.DID, error) {
	var err error
	var entry *HandleEntry
	d.mutex.RLock()
	maybeEntry, ok := d.handleCache[h]
	d.mutex.RUnlock()

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

	d.mutex.Lock()
	d.identityCache[did] = entry
	if he != nil {
		d.handleCache[ident.Handle] = *he
	}
	d.mutex.Unlock()
	return &entry, nil
}

func (d *CacheDirectory) LookupDID(ctx context.Context, did syntax.DID) (*Identity, error) {
	var err error
	var entry *IdentityEntry
	d.mutex.RLock()
	maybeEntry, ok := d.identityCache[did]
	d.mutex.RUnlock()

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
