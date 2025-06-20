package identity

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gander-social/gander-indigo-sovereign/atproto/syntax"

	"github.com/hashicorp/golang-lru/v2/expirable"
)

// CacheDirectory is an implementation of identity.Directory with local cache of Handle and DID
type CacheDirectory struct {
	Inner             Directory
	ErrTTL            time.Duration
	InvalidHandleTTL  time.Duration
	handleCache       *expirable.LRU[syntax.Handle, handleEntry]
	identityCache     *expirable.LRU[syntax.DID, identityEntry]
	didLookupChans    sync.Map
	handleLookupChans sync.Map
}

type handleEntry struct {
	Updated time.Time
	DID     syntax.DID
	Err     error
}

type identityEntry struct {
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
		handleCache:      expirable.NewLRU[syntax.Handle, handleEntry](capacity, nil, hitTTL),
		identityCache:    expirable.NewLRU[syntax.DID, identityEntry](capacity, nil, hitTTL),
	}
}

func (d *CacheDirectory) isHandleStale(e *handleEntry) bool {
	if e.Err != nil && time.Since(e.Updated) > d.ErrTTL {
		return true
	}
	return false
}

func (d *CacheDirectory) isIdentityStale(e *identityEntry) bool {
	if e.Err != nil && time.Since(e.Updated) > d.ErrTTL {
		return true
	}
	if e.Identity != nil && e.Identity.Handle.IsInvalidHandle() && time.Since(e.Updated) > d.InvalidHandleTTL {
		return true
	}
	return false
}

func (d *CacheDirectory) updateHandle(ctx context.Context, h syntax.Handle) handleEntry {
	ident, err := d.Inner.LookupHandle(ctx, h)
	if err != nil {
		he := handleEntry{
			Updated: time.Now(),
			DID:     "",
			Err:     err,
		}
		d.handleCache.Add(h, he)
		return he
	}

	entry := identityEntry{
		Updated:  time.Now(),
		Identity: ident,
		Err:      nil,
	}
	he := handleEntry{
		Updated: time.Now(),
		DID:     ident.DID,
		Err:     nil,
	}

	d.identityCache.Add(ident.DID, entry)
	d.handleCache.Add(ident.Handle, he)
	return he
}

func (d *CacheDirectory) ResolveHandle(ctx context.Context, h syntax.Handle) (syntax.DID, error) {
	h = h.Normalize()
	if h.IsInvalidHandle() {
		return "", fmt.Errorf("can not resolve handle: %w", ErrInvalidHandle)
	}
	start := time.Now()
	entry, ok := d.handleCache.Get(h)
	if ok && !d.isHandleStale(&entry) {
		handleCacheHits.Inc()
		handleResolution.WithLabelValues("lru", "cached").Inc()
		handleResolutionDuration.WithLabelValues("lru", "cached").Observe(time.Since(start).Seconds())
		return entry.DID, entry.Err
	}
	handleCacheMisses.Inc()

	// Coalesce multiple requests for the same Handle
	res := make(chan struct{})
	val, loaded := d.handleLookupChans.LoadOrStore(h.String(), res)
	if loaded {
		handleRequestsCoalesced.Inc()
		handleResolution.WithLabelValues("lru", "coalesced").Inc()
		handleResolutionDuration.WithLabelValues("lru", "coalesced").Observe(time.Since(start).Seconds())
		// Wait for the result from the pending request
		select {
		case <-val.(chan struct{}):
			// The result should now be in the cache
			entry, ok := d.handleCache.Get(h)
			if ok && !d.isHandleStale(&entry) {
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
		handleResolution.WithLabelValues("lru", "error").Inc()
		handleResolutionDuration.WithLabelValues("lru", "error").Observe(time.Since(start).Seconds())
		return "", newEntry.Err
	}
	if newEntry.DID != "" {
		handleResolution.WithLabelValues("lru", "success").Inc()
		handleResolutionDuration.WithLabelValues("lru", "success").Observe(time.Since(start).Seconds())
		return newEntry.DID, nil
	}
	return "", fmt.Errorf("unexpected control-flow error")
}

func (d *CacheDirectory) updateDID(ctx context.Context, did syntax.DID) identityEntry {
	ident, err := d.Inner.LookupDID(ctx, did)
	// persist the identity lookup error, instead of processing it immediately
	entry := identityEntry{
		Updated:  time.Now(),
		Identity: ident,
		Err:      err,
	}
	var he *handleEntry
	// if *not* an error, then also update the handle cache
	if nil == err && !ident.Handle.IsInvalidHandle() {
		he = &handleEntry{
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
	start := time.Now()
	entry, ok := d.identityCache.Get(did)
	if ok && !d.isIdentityStale(&entry) {
		identityCacheHits.Inc()
		didResolution.WithLabelValues("lru", "cached").Inc()
		didResolutionDuration.WithLabelValues("lru", "cached").Observe(time.Since(start).Seconds())
		return entry.Identity, true, entry.Err
	}
	identityCacheMisses.Inc()

	// Coalesce multiple requests for the same DID
	res := make(chan struct{})
	val, loaded := d.didLookupChans.LoadOrStore(did.String(), res)
	if loaded {
		identityRequestsCoalesced.Inc()
		didResolution.WithLabelValues("lru", "coalesced").Inc()
		didResolutionDuration.WithLabelValues("lru", "coalesced").Observe(time.Since(start).Seconds())
		// Wait for the result from the pending request
		select {
		case <-val.(chan struct{}):
			// The result should now be in the cache
			entry, ok := d.identityCache.Get(did)
			if ok && !d.isIdentityStale(&entry) {
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
		didResolution.WithLabelValues("lru", "error").Inc()
		didResolutionDuration.WithLabelValues("lru", "error").Observe(time.Since(start).Seconds())
		return nil, false, newEntry.Err
	}
	if newEntry.Identity != nil {
		didResolution.WithLabelValues("lru", "success").Inc()
		didResolutionDuration.WithLabelValues("lru", "success").Observe(time.Since(start).Seconds())
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
	// NOTE: DeclaredHandle() returns a normalized handle, and we already normalized 'h' above
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

func (d *CacheDirectory) Purge(ctx context.Context, atid syntax.AtIdentifier) error {
	handle, err := atid.AsHandle()
	if nil == err { // if not an error, is a handle
		handle = handle.Normalize()
		d.handleCache.Remove(handle)
		return nil
	}
	did, err := atid.AsDID()
	if nil == err { // if not an error, is a DID
		d.identityCache.Remove(did)
		return nil
	}
	return fmt.Errorf("at-identifier neither a Handle nor a DID")
}
