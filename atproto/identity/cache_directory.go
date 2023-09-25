package identity

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/hashicorp/golang-lru/v2/expirable"
)

type CacheDirectory struct {
	Inner             Directory
	ErrTTL            time.Duration
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

var handleCacheHits = promauto.NewCounter(prometheus.CounterOpts{
	Name: "atproto_directory_handle_cache_hits",
	Help: "Number of cache hits for ATProto handle lookups",
})

var handleCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
	Name: "atproto_directory_handle_cache_misses",
	Help: "Number of cache misses for ATProto handle lookups",
})

var identityCacheHits = promauto.NewCounter(prometheus.CounterOpts{
	Name: "atproto_directory_identity_cache_hits",
	Help: "Number of cache hits for ATProto identity lookups",
})

var identityCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
	Name: "atproto_directory_identity_cache_misses",
	Help: "Number of cache misses for ATProto identity lookups",
})

var identityRequestsCoalesced = promauto.NewCounter(prometheus.CounterOpts{
	Name: "atproto_directory_identity_requests_coalesced",
	Help: "Number of identity requests coalesced",
})

var handleRequestsCoalesced = promauto.NewCounter(prometheus.CounterOpts{
	Name: "atproto_directory_handle_requests_coalesced",
	Help: "Number of handle requests coalesced",
})

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

func (d *CacheDirectory) coalescedResolveHandle(ctx context.Context, handle syntax.Handle) (syntax.DID, error) {
	resC := make(chan syntax.DID, 1)
	errC := make(chan error, 1)
	actualLookup := false

	val, loaded := d.handleLookupChans.LoadOrStore(handle.String(), resC)
	if loaded {
		handleRequestsCoalesced.Inc()
		// Wait for the result from the original goroutine
		select {
		case <-val.(chan *Identity):
			// The result should now be in the cache
			entry, ok := d.handleCache.Get(handle)
			if ok && !d.IsHandleStale(&entry) {
				return entry.DID, entry.Err
			}
			return "", fmt.Errorf("handle not found")
		case err := <-errC:
			return "", err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	} else {
		actualLookup = true
	}

	var did syntax.DID
	var err error

	// Perform actual lookup only if this goroutine is the one doing it
	if actualLookup {
		entry, err := d.updateHandle(ctx, handle)
		if err != nil {
			errC <- err
		} else {
			if entry != nil {
				did = entry.DID
			}
			resC <- did
		}
		// Cleanup after sending result or error
		d.handleLookupChans.Delete(handle.String())
		close(resC)
		close(errC)
	}

	return did, err
}

func (d *CacheDirectory) ResolveHandle(ctx context.Context, h syntax.Handle) (syntax.DID, error) {
	entry, ok := d.handleCache.Get(h)
	if ok && !d.IsHandleStale(&entry) {
		handleCacheHits.Inc()
		return entry.DID, entry.Err
	}
	handleCacheMisses.Inc()

	did, err := d.coalescedResolveHandle(ctx, h)
	if err != nil {
		return "", err
	}
	return did, nil
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

func (d *CacheDirectory) coalescedResolveDID(ctx context.Context, did syntax.DID) (*Identity, error) {
	resC := make(chan *Identity, 1)
	errC := make(chan error, 1)
	actualLookup := false

	val, loaded := d.didLookupChans.LoadOrStore(did.String(), resC)
	if loaded {
		identityRequestsCoalesced.Inc()
		// Wait for the result from the original goroutine
		select {
		case <-val.(chan *Identity):
			// The result should now be in the cache
			entry, ok := d.identityCache.Get(did)
			if ok && !d.IsIdentityStale(&entry) {
				return entry.Identity, entry.Err
			}
			return nil, fmt.Errorf("identity not found")
		case err := <-errC:
			return nil, err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	} else {
		actualLookup = true
	}

	var doc *Identity
	var err error

	// Perform actual lookup only if this goroutine is the one doing it
	if actualLookup {
		entry, err := d.updateDID(ctx, did)
		if err != nil {
			errC <- err
		} else {
			if entry != nil {
				doc = entry.Identity
			}
			resC <- doc
		}
		// Cleanup after sending result or error
		d.didLookupChans.Delete(did.String())
		close(resC)
		close(errC)
	}

	return doc, err
}

func (d *CacheDirectory) LookupDID(ctx context.Context, did syntax.DID) (*Identity, error) {
	entry, ok := d.identityCache.Get(did)
	if ok && !d.IsIdentityStale(&entry) {
		identityCacheHits.Inc()
		return entry.Identity, entry.Err
	}
	identityCacheMisses.Inc()

	doc, err := d.coalescedResolveDID(ctx, did)
	if err != nil {
		return nil, err
	}
	return doc, nil
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
