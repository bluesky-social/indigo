package identity

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/bluesky-social/indigo/cmd/butterfly/store"
)

const identityCache = "identity-cache"
const handleCache = "handle-cache"

// StoreDirectory is an implementation of identity.Directory with cache of Handle and DID in the provided Store
type StoreDirectory struct {
	Inner             identity.Directory
	ErrTTL            time.Duration
	InvalidHandleTTL  time.Duration
	store             store.Store
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
	Identity *identity.Identity
	Err      error
}

// var _ identity.Directory = (*StoreDirectory)(nil) TODO is this needed?

// Ttl of zero means unlimited duration.
func NewStoreDirectory(inner identity.Directory, store store.Store, hitTTL, errTTL, invalidHandleTTL time.Duration) StoreDirectory {
	return StoreDirectory{
		ErrTTL:           errTTL,
		InvalidHandleTTL: invalidHandleTTL,
		Inner:            inner,
		store:            store,
	}
}

func (d *StoreDirectory) isHandleStale(e *handleEntry) bool {
	if e.Err != nil && time.Since(e.Updated) > d.ErrTTL {
		return true
	}
	return false
}

func (d *StoreDirectory) isIdentityStale(e *identityEntry) bool {
	if e.Err != nil && time.Since(e.Updated) > d.ErrTTL {
		return true
	}
	if e.Identity != nil && e.Identity.Handle.IsInvalidHandle() && time.Since(e.Updated) > d.InvalidHandleTTL {
		return true
	}
	return false
}

func (d *StoreDirectory) updateHandle(ctx context.Context, h syntax.Handle) handleEntry {
	ident, err := d.Inner.LookupHandle(ctx, h)
	if err != nil {
		he := handleEntry{
			Updated: time.Now(),
			DID:     "",
			Err:     err,
		}
		putHandle(d.store, h, &he)
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

	putIdent(d.store, ident.DID, &entry)
	putHandle(d.store, ident.Handle, &he)
	return he
}

func (d *StoreDirectory) ResolveHandle(ctx context.Context, h syntax.Handle) (syntax.DID, error) {
	h = h.Normalize()
	if h.IsInvalidHandle() {
		return "", fmt.Errorf("can not resolve handle: %w", identity.ErrInvalidHandle)
	}
	// start := time.Now() TODO
	entry, err := getHandle(d.store, h)
	if err == nil && !d.isHandleStale(entry) {
		// TODO
		// handleCacheHits.Inc()
		// handleResolution.WithLabelValues("lru", "cached").Inc()
		// handleResolutionDuration.WithLabelValues("lru", "cached").Observe(time.Since(start).Seconds())
		return entry.DID, entry.Err
	}
	// handleCacheMisses.Inc() TODO

	// Coalesce multiple requests for the same Handle
	res := make(chan struct{})
	val, loaded := d.handleLookupChans.LoadOrStore(h.String(), res)
	if loaded {
		// TODO
		// handleRequestsCoalesced.Inc()
		// handleResolution.WithLabelValues("lru", "coalesced").Inc()
		// handleResolutionDuration.WithLabelValues("lru", "coalesced").Observe(time.Since(start).Seconds())
		// Wait for the result from the pending request
		select {
		case <-val.(chan struct{}):
			// The result should now be in the cache
			entry, err := getHandle(d.store, h)
			if err == nil && !d.isHandleStale(entry) {
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
		// TODO
		// handleResolution.WithLabelValues("lru", "error").Inc()
		// handleResolutionDuration.WithLabelValues("lru", "error").Observe(time.Since(start).Seconds())
		return "", newEntry.Err
	}
	if newEntry.DID != "" {
		// TODO
		// handleResolution.WithLabelValues("lru", "success").Inc()
		// handleResolutionDuration.WithLabelValues("lru", "success").Observe(time.Since(start).Seconds())
		return newEntry.DID, nil
	}
	return "", fmt.Errorf("unexpected control-flow error")
}

func (d *StoreDirectory) updateDID(ctx context.Context, did syntax.DID) identityEntry {
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

	putIdent(d.store, did, &entry)
	if he != nil {
		putHandle(d.store, ident.Handle, he)
	}
	return entry
}

func (d *StoreDirectory) LookupDID(ctx context.Context, did syntax.DID) (*identity.Identity, error) {
	id, _, err := d.LookupDIDWithCacheState(ctx, did)
	return id, err
}

func (d *StoreDirectory) LookupDIDWithCacheState(ctx context.Context, did syntax.DID) (*identity.Identity, bool, error) {
	// start := time.Now() TODO
	entry, err := getIdent(d.store, did)
	if err == nil && !d.isIdentityStale(entry) {
		// TODO
		// identityCacheHits.Inc()
		// didResolution.WithLabelValues("lru", "cached").Inc()
		// didResolutionDuration.WithLabelValues("lru", "cached").Observe(time.Since(start).Seconds())
		return entry.Identity, true, entry.Err
	}
	// identityCacheMisses.Inc() TODO

	// Coalesce multiple requests for the same DID
	res := make(chan struct{})
	val, loaded := d.didLookupChans.LoadOrStore(did.String(), res)
	if loaded {
		// TODO
		// identityRequestsCoalesced.Inc()
		// didResolution.WithLabelValues("lru", "coalesced").Inc()
		// didResolutionDuration.WithLabelValues("lru", "coalesced").Observe(time.Since(start).Seconds())
		// Wait for the result from the pending request
		select {
		case <-val.(chan struct{}):
			// The result should now be in the cache
			entry, err := getIdent(d.store, did)
			if err == nil && !d.isIdentityStale(entry) {
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
		// TODO
		// didResolution.WithLabelValues("lru", "error").Inc()
		// didResolutionDuration.WithLabelValues("lru", "error").Observe(time.Since(start).Seconds())
		return nil, false, newEntry.Err
	}
	if newEntry.Identity != nil {
		// TODO
		// didResolution.WithLabelValues("lru", "success").Inc()
		// didResolutionDuration.WithLabelValues("lru", "success").Observe(time.Since(start).Seconds())
		return newEntry.Identity, false, nil
	}
	return nil, false, fmt.Errorf("unexpected control-flow error")
}

func (d *StoreDirectory) LookupHandle(ctx context.Context, h syntax.Handle) (*identity.Identity, error) {
	ident, _, err := d.LookupHandleWithCacheState(ctx, h)
	return ident, err
}

func (d *StoreDirectory) LookupHandleWithCacheState(ctx context.Context, h syntax.Handle) (*identity.Identity, bool, error) {
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
		return nil, hit, fmt.Errorf("%w: %s != %s", identity.ErrHandleMismatch, declared, h)
	}
	return ident, hit, nil
}

func (d *StoreDirectory) Lookup(ctx context.Context, a syntax.AtIdentifier) (*identity.Identity, error) {
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

func (d *StoreDirectory) Purge(ctx context.Context, atid syntax.AtIdentifier) error {
	handle, err := atid.AsHandle()
	if nil == err { // if not an error, is a handle
		handle = handle.Normalize()
		delHandle(d.store, handle)
		return nil
	}
	did, err := atid.AsDID()
	if nil == err { // if not an error, is a DID
		delIdent(d.store, did)
		return nil
	}
	return fmt.Errorf("at-identifier neither a Handle nor a DID")
}

func getHandle(store store.Store, handle syntax.Handle) (*handleEntry, error) {
	entryJSON, err := store.KvGet(handleCache, string(handle))
	if entryJSON != "" {
		// TODO - is this parse safe? do we need to be checking the output or anything?
		var entry handleEntry
		if err := json.Unmarshal([]byte(entryJSON), &entry); err != nil {
			return nil, err
		}
		return &entry, nil
	}
	return nil, err
}

func putHandle(store store.Store, handle syntax.Handle, entry *handleEntry) error {
	// TODO - is this right?
	entryJSON, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return store.KvPut(handleCache, string(handle), string(entryJSON))
}

func delHandle(store store.Store, handle syntax.Handle) error {
	return store.KvDel(handleCache, string(handle))
}

func getIdent(store store.Store, did syntax.DID) (*identityEntry, error) {
	entryJSON, err := store.KvGet(identityCache, string(did))
	if entryJSON != "" {
		// TODO - is this parse safe? do we need to be checking the output or anything?
		var entry identityEntry
		if err := json.Unmarshal([]byte(entryJSON), &entry); err != nil {
			return nil, err
		}
		return &entry, nil
	}
	return nil, err
}

func putIdent(store store.Store, did syntax.DID, entry *identityEntry) error {
	// TODO - is this right?
	entryJSON, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return store.KvPut(identityCache, string(did), string(entryJSON))
}

func delIdent(store store.Store, did syntax.DID) error {
	return store.KvDel(identityCache, string(did))
}
