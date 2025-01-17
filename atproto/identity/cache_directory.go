package identity

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/hashicorp/golang-lru/v2/expirable"
)

type CacheDirectory struct {
	Inner             Directory
	ErrTTL            time.Duration
	InvalidHandleTTL  time.Duration
	handleCache       *expirable.LRU[syntax.Handle, HandleEntry]
	identityCache     *expirable.LRU[syntax.DID, IdentityEntry]
	didLookupChans    sync.Map
	handleLookupChans sync.Map
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
func NewCacheDirectory(inner Directory, capacity int, hitTTL, errTTL, invalidHandleTTL time.Duration) CacheDirectory {
	return CacheDirectory{
		ErrTTL:           errTTL,
		InvalidHandleTTL: invalidHandleTTL,
		Inner:            inner,
		handleCache:      expirable.NewLRU[syntax.Handle, HandleEntry](capacity, nil, hitTTL),
		identityCache:    expirable.NewLRU[syntax.DID, IdentityEntry](capacity, nil, hitTTL),
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
	if e.Identity != nil && e.Identity.Handle.IsInvalidHandle() && time.Since(e.Updated) > d.InvalidHandleTTL {
		return true
	}
	return false
}

func (d *CacheDirectory) updateHandle(ctx context.Context, h syntax.Handle) HandleEntry {
	ident, err := d.Inner.LookupHandle(ctx, h)
	if err != nil {
		he := HandleEntry{
			Updated: time.Now(),
			DID:     "",
			Err:     err,
		}
		d.handleCache.Add(h, he)
		return he
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
	return he
}

func (d *CacheDirectory) ResolveHandle(ctx context.Context, h syntax.Handle) (syntax.DID, error) {
	if h.IsInvalidHandle() {
		return "", fmt.Errorf("can not resolve handle: %w", ErrInvalidHandle)
	}
	entry, ok := d.handleCache.Get(h)
	if ok && !d.IsHandleStale(&entry) {
		handleCacheHits.Inc()
		return entry.DID, entry.Err
	}
	handleCacheMisses.Inc()

	// Coalesce multiple requests for the same Handle
	res := make(chan struct{})
	val, loaded := d.handleLookupChans.LoadOrStore(h.String(), res)
	if loaded {
		handleRequestsCoalesced.Inc()
		// Wait for the result from the pending request
		select {
		case <-val.(chan struct{}):
			// The result should now be in the cache
			entry, ok := d.handleCache.Get(h)
			if ok && !d.IsHandleStale(&entry) {
				return entry.DID, entry.Err
			}
			return "", fmt.Errorf("identity not found in cache after coalesce returned")
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	// Update the Handle Entry from PLC and cache the result
	newEntry := d.updateHandle(ctx, h)

	// Cleanup the coalesce map and close the results channel
	d.handleLookupChans.Delete(h.String())
	// Callers waiting will now get the result from the cache
	close(res)

	if newEntry.Err != nil {
		return "", newEntry.Err
	}
	if newEntry.DID != "" {
		return newEntry.DID, nil
	}
	return "", fmt.Errorf("unexpected control-flow error")
}

func (d *CacheDirectory) updateDID(ctx context.Context, did syntax.DID) IdentityEntry {
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
	return entry
}

func (d *CacheDirectory) LookupDID(ctx context.Context, did syntax.DID) (*Identity, error) {
	id, _, err := d.LookupDIDWithCacheState(ctx, did)
	return id, err
}

func (d *CacheDirectory) LookupDIDWithCacheState(ctx context.Context, did syntax.DID) (*Identity, bool, error) {
	entry, ok := d.identityCache.Get(did)
	if ok && !d.IsIdentityStale(&entry) {
		identityCacheHits.Inc()
		return entry.Identity, true, entry.Err
	}
	identityCacheMisses.Inc()

	// Coalesce multiple requests for the same DID
	res := make(chan struct{})
	val, loaded := d.didLookupChans.LoadOrStore(did.String(), res)
	if loaded {
		identityRequestsCoalesced.Inc()
		// Wait for the result from the pending request
		select {
		case <-val.(chan struct{}):
			// The result should now be in the cache
			entry, ok := d.identityCache.Get(did)
			if ok && !d.IsIdentityStale(&entry) {
				return entry.Identity, false, entry.Err
			}
			return nil, false, fmt.Errorf("identity not found in cache after coalesce returned")
		case <-ctx.Done():
			return nil, false, ctx.Err()
		}
	}

	// Update the Identity Entry from PLC and cache the result
	newEntry := d.updateDID(ctx, did)

	// Cleanup the coalesce map and close the results channel
	d.didLookupChans.Delete(did.String())
	// Callers waiting will now get the result from the cache
	close(res)

	if newEntry.Err != nil {
		return nil, false, newEntry.Err
	}
	if newEntry.Identity != nil {
		return newEntry.Identity, false, nil
	}
	return nil, false, fmt.Errorf("unexpected control-flow error")
}

func (d *CacheDirectory) LookupHandle(ctx context.Context, h syntax.Handle) (*Identity, error) {
	ident, _, err := d.LookupHandleWithCacheState(ctx, h)
	return ident, err
}

func (d *CacheDirectory) LookupHandleWithCacheState(ctx context.Context, h syntax.Handle) (*Identity, bool, error) {
	h = h.Normalize()
	did, err := d.ResolveHandle(ctx, h)
	if err != nil {
		return nil, false, err
	}
	ident, hit, err := d.LookupDIDWithCacheState(ctx, did)
	if err != nil {
		return nil, hit, err
	}

	declared, err := ident.DeclaredHandle()
	if err != nil {
		return nil, hit, fmt.Errorf("could not verify handle/DID mapping: %w", err)
	}
	if declared != h {
		return nil, hit, fmt.Errorf("%w: %s != %s", ErrHandleMismatch, declared, h)
	}
	return ident, hit, nil
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
		handle = handle.Normalize()
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
