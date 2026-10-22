package identity

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/hashicorp/golang-lru/v2/expirable"
)

// CacheDirectory is an implementation of identity.Directory with local cache of Handle and DID
type CacheDirectory struct {
	Inner            Directory
	ErrTTL           time.Duration
	InvalidHandleTTL time.Duration
	handleCache      *expirable.LRU[syntax.Handle, handleEntry]
	identityCache    *expirable.LRU[syntax.DID, identityEntry]
	mu               sync.Mutex
	generation       uint64
	didLookups       map[syntax.DID]*cacheLookup[identityEntry]
	handleLookups    map[syntax.Handle]*cacheLookup[handleEntry]
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

type cacheLookup[T any] struct {
	generation uint64
	done       chan struct{}
	entry      T
}

var _ Directory = (*CacheDirectory)(nil)

// Capacity of zero means unlimited size. Similarly, ttl of zero means unlimited duration.
func NewCacheDirectory(inner Directory, capacity int, hitTTL, errTTL, invalidHandleTTL time.Duration) *CacheDirectory {
	return &CacheDirectory{
		ErrTTL:           errTTL,
		InvalidHandleTTL: invalidHandleTTL,
		Inner:            inner,
		handleCache:      expirable.NewLRU[syntax.Handle, handleEntry](capacity, nil, hitTTL),
		identityCache:    expirable.NewLRU[syntax.DID, identityEntry](capacity, nil, hitTTL),
		didLookups:       make(map[syntax.DID]*cacheLookup[identityEntry]),
		handleLookups:    make(map[syntax.Handle]*cacheLookup[handleEntry]),
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

func (d *CacheDirectory) updateHandle(ctx context.Context, h syntax.Handle, generation uint64) handleEntry {
	ident, err := d.Inner.LookupHandle(ctx, h)
	if err != nil {
		he := handleEntry{
			Updated: time.Now(),
			DID:     "",
			Err:     err,
		}
		// don't cache ctx cancelation or timeout errors
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			d.mu.Lock()
			if generation == d.generation {
				d.handleCache.Add(h, he)
			}
			d.mu.Unlock()
		}
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

	d.mu.Lock()
	if generation == d.generation {
		d.identityCache.Add(ident.DID, entry)
		d.handleCache.Add(ident.Handle, he)
	}
	d.mu.Unlock()
	return he
}

func (d *CacheDirectory) ResolveHandle(ctx context.Context, h syntax.Handle) (syntax.DID, error) {
	h = h.Normalize()
	if h.IsInvalidHandle() {
		return "", fmt.Errorf("can not resolve handle: %w", ErrInvalidHandle)
	}
	start := time.Now()
	d.mu.Lock()
	entry, ok := d.handleCache.Get(h)
	if ok && !d.isHandleStale(&entry) {
		d.mu.Unlock()
		handleCacheHits.Inc()
		handleResolution.WithLabelValues("lru", "cached").Inc()
		handleResolutionDuration.WithLabelValues("lru", "cached").Observe(time.Since(start).Seconds())
		return entry.DID, entry.Err
	}
	pending := d.handleLookups[h]
	coalesced := pending != nil && pending.generation == d.generation
	if !coalesced {
		pending = &cacheLookup[handleEntry]{generation: d.generation, done: make(chan struct{})}
		d.handleLookups[h] = pending
	}
	d.mu.Unlock()
	handleCacheMisses.Inc()

	// Coalesce only work started after the same most recent purge.
	if coalesced {
		handleRequestsCoalesced.Inc()
		handleResolution.WithLabelValues("lru", "coalesced").Inc()
		handleResolutionDuration.WithLabelValues("lru", "coalesced").Observe(time.Since(start).Seconds())
		select {
		case <-pending.done:
			return pending.entry.DID, pending.entry.Err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	newEntry := d.updateHandle(ctx, h, pending.generation)
	d.mu.Lock()
	pending.entry = newEntry
	if d.handleLookups[h] == pending {
		delete(d.handleLookups, h)
	}
	close(pending.done)
	d.mu.Unlock()

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

func (d *CacheDirectory) updateDID(ctx context.Context, did syntax.DID, generation uint64) identityEntry {
	ident, err := d.Inner.LookupDID(ctx, did)
	// persist the identity lookup error, instead of processing it immediately
	entry := identityEntry{
		Updated:  time.Now(),
		Identity: ident,
		Err:      err,
	}

	// don't cache errors caused by ctx cancellation or timeouts
	if err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		return entry
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

	d.mu.Lock()
	if generation == d.generation {
		d.identityCache.Add(did, entry)
		if he != nil {
			d.handleCache.Add(ident.Handle, *he)
		}
	}
	d.mu.Unlock()
	return entry
}

func (d *CacheDirectory) LookupDID(ctx context.Context, did syntax.DID) (*Identity, error) {
	id, _, err := d.LookupDIDWithCacheState(ctx, did)
	return id, err
}

func (d *CacheDirectory) LookupDIDWithCacheState(ctx context.Context, did syntax.DID) (*Identity, bool, error) {
	start := time.Now()
	d.mu.Lock()
	entry, ok := d.identityCache.Get(did)
	if ok && !d.isIdentityStale(&entry) {
		d.mu.Unlock()
		identityCacheHits.Inc()
		didResolution.WithLabelValues("lru", "cached").Inc()
		didResolutionDuration.WithLabelValues("lru", "cached").Observe(time.Since(start).Seconds())
		return entry.Identity, true, entry.Err
	}
	pending := d.didLookups[did]
	coalesced := pending != nil && pending.generation == d.generation
	if !coalesced {
		pending = &cacheLookup[identityEntry]{generation: d.generation, done: make(chan struct{})}
		d.didLookups[did] = pending
	}
	d.mu.Unlock()
	identityCacheMisses.Inc()

	// Coalesce only work started after the same most recent purge.
	if coalesced {
		identityRequestsCoalesced.Inc()
		didResolution.WithLabelValues("lru", "coalesced").Inc()
		didResolutionDuration.WithLabelValues("lru", "coalesced").Observe(time.Since(start).Seconds())
		select {
		case <-pending.done:
			return pending.entry.Identity, false, pending.entry.Err
		case <-ctx.Done():
			return nil, false, ctx.Err()
		}
	}

	newEntry := d.updateDID(ctx, did, pending.generation)
	d.mu.Lock()
	pending.entry = newEntry
	if d.didLookups[did] == pending {
		delete(d.didLookups, did)
	}
	close(pending.done)
	d.mu.Unlock()

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

// Purge also fences pre-purge work from coalescing with subsequent requests or
// publishing into either cache. A shared generation covers handle lookups whose
// DID is not known until they finish; unrelated resident entries remain cached.
// Callers already waiting before a purge may still receive their original result.
func (d *CacheDirectory) Purge(ctx context.Context, atid syntax.AtIdentifier) error {
	handle, err := atid.AsHandle()
	if nil == err { // if not an error, is a handle
		handle = handle.Normalize()
		d.mu.Lock()
		d.generation++
		d.handleCache.Remove(handle)
		d.mu.Unlock()
		return nil
	}
	did, err := atid.AsDID()
	if nil == err { // if not an error, is a DID
		d.mu.Lock()
		d.generation++
		d.identityCache.Remove(did)
		d.mu.Unlock()
		return nil
	}
	return fmt.Errorf("at-identifier neither a Handle nor a DID")
}
