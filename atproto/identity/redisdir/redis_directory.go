package redisdir

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/go-redis/cache/v9"
	"github.com/redis/go-redis/v9"
)

// prefix string for all the Redis keys this cache uses
var redisDirPrefix string = "dir/"

// Uses redis as a cache for identity lookups.
//
// Includes an in-process LRU cache as well (provided by the redis client library), for hot key (identities).
type RedisDirectory struct {
	Inner            identity.Directory
	ErrTTL           time.Duration
	HitTTL           time.Duration
	InvalidHandleTTL time.Duration

	handleCache       *cache.Cache
	identityCache     *cache.Cache
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

var _ identity.Directory = (*RedisDirectory)(nil)

// Creates a new caching `identity.Directory` wrapper around an existing directory, using Redis and in-process LRU for caching.
//
// `redisURL` contains all the redis connection config options.
// `hitTTL` and `errTTL` define how long successful and errored identity metadata should be cached (respectively). errTTL is expected to be shorted than hitTTL.
// `lruSize` is the size of the in-process cache, for each of the handle and identity caches. 10000 is a reasonable default.
func NewRedisDirectory(inner identity.Directory, redisURL string, hitTTL, errTTL, invalidHandleTTL time.Duration, lruSize int) (*RedisDirectory, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	rdb := redis.NewClient(opt)
	// check redis connection
	_, err = rdb.Ping(context.TODO()).Result()
	if err != nil {
		return nil, err
	}
	handleCache := cache.New(&cache.Options{
		Redis:      rdb,
		LocalCache: cache.NewTinyLFU(lruSize, hitTTL),
	})
	identityCache := cache.New(&cache.Options{
		Redis:      rdb,
		LocalCache: cache.NewTinyLFU(lruSize, hitTTL),
	})
	return &RedisDirectory{
		Inner:            inner,
		ErrTTL:           errTTL,
		HitTTL:           hitTTL,
		InvalidHandleTTL: invalidHandleTTL,
		handleCache:      handleCache,
		identityCache:    identityCache,
	}, nil
}

func (d *RedisDirectory) isHandleStale(e *handleEntry) bool {
	if e.Err != nil && time.Since(e.Updated) > d.ErrTTL {
		return true
	}
	return false
}

func (d *RedisDirectory) isIdentityStale(e *identityEntry) bool {
	if e.Err != nil && time.Since(e.Updated) > d.ErrTTL {
		return true
	}
	if e.Identity != nil && e.Identity.Handle.IsInvalidHandle() && time.Since(e.Updated) > d.InvalidHandleTTL {
		return true
	}
	return false
}

func (d *RedisDirectory) updateHandle(ctx context.Context, h syntax.Handle) handleEntry {
	h = h.Normalize()
	ident, err := d.Inner.LookupHandle(ctx, h)
	if err != nil {
		he := handleEntry{
			Updated: time.Now(),
			DID:     "",
			Err:     err,
		}
		err = d.handleCache.Set(&cache.Item{
			Ctx:   ctx,
			Key:   redisDirPrefix + h.String(),
			Value: he,
			TTL:   d.ErrTTL,
		})
		if err != nil {
			he.DID = ""
			he.Err = err
			return he
		}
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

	err = d.identityCache.Set(&cache.Item{
		Ctx:   ctx,
		Key:   redisDirPrefix + ident.DID.String(),
		Value: entry,
		TTL:   d.HitTTL,
	})
	if err != nil {
		he.DID = ""
		he.Err = err
		return he
	}
	err = d.handleCache.Set(&cache.Item{
		Ctx:   ctx,
		Key:   redisDirPrefix + h.String(),
		Value: he,
		TTL:   d.HitTTL,
	})
	if err != nil {
		he.DID = ""
		he.Err = err
		return he
	}
	return he
}

func (d *RedisDirectory) ResolveHandle(ctx context.Context, h syntax.Handle) (syntax.DID, error) {
	var entry handleEntry
	err := d.handleCache.Get(ctx, redisDirPrefix+h.String(), &entry)
	if err != nil && err != cache.ErrCacheMiss {
		return "", err
	}
	if err != cache.ErrCacheMiss && !d.isHandleStale(&entry) {
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
			err := d.handleCache.Get(ctx, redisDirPrefix+h.String(), entry)
			if err != nil && err != cache.ErrCacheMiss {
				return "", err
			}
			if err != cache.ErrCacheMiss && !d.isHandleStale(&entry) {
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

func (d *RedisDirectory) updateDID(ctx context.Context, did syntax.DID) identityEntry {
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

	err = d.identityCache.Set(&cache.Item{
		Ctx:   ctx,
		Key:   redisDirPrefix + did.String(),
		Value: entry,
		TTL:   d.HitTTL,
	})
	if err != nil {
		entry.Identity = nil
		entry.Err = err
		return entry
	}
	if he != nil {
		err = d.handleCache.Set(&cache.Item{
			Ctx:   ctx,
			Key:   redisDirPrefix + ident.Handle.String(),
			Value: *he,
			TTL:   d.HitTTL,
		})
		if err != nil {
			entry.Identity = nil
			entry.Err = err
			return entry
		}
	}
	return entry
}

func (d *RedisDirectory) LookupDID(ctx context.Context, did syntax.DID) (*identity.Identity, error) {
	var entry identityEntry
	err := d.identityCache.Get(ctx, redisDirPrefix+did.String(), &entry)
	if err != nil && err != cache.ErrCacheMiss {
		return nil, err
	}
	if err != cache.ErrCacheMiss && !d.isIdentityStale(&entry) {
		identityCacheHits.Inc()
		return entry.Identity, entry.Err
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
			err = d.identityCache.Get(ctx, redisDirPrefix+did.String(), &entry)
			if err != nil && err != cache.ErrCacheMiss {
				return nil, err
			}
			if err != cache.ErrCacheMiss && !d.isIdentityStale(&entry) {
				return entry.Identity, entry.Err
			}
			return nil, fmt.Errorf("identity not found in cache after coalesce returned")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	var doc *identity.Identity
	// Update the Identity Entry from PLC and cache the result
	newEntry, err := d.updateDID(ctx, did)
	if err == nil && newEntry != nil {
		doc = newEntry.Identity
	}
	// Cleanup the coalesce map and close the results channel
	d.didLookupChans.Delete(did.String())
	// Callers waiting will now get the result from the cache
	close(res)

	return doc, err
}

func (d *RedisDirectory) LookupHandle(ctx context.Context, h syntax.Handle) (*identity.Identity, error) {
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

func (d *RedisDirectory) Lookup(ctx context.Context, a syntax.AtIdentifier) (*identity.Identity, error) {
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

func (d *RedisDirectory) Purge(ctx context.Context, a syntax.AtIdentifier) error {
	handle, err := a.AsHandle()
	if nil == err { // if not an error, is a handle
		handle = handle.Normalize()
		err = d.handleCache.Delete(ctx, handle.String())
		if err == cache.ErrCacheMiss {
			return nil
		}
		return err
	}
	did, err := a.AsDID()
	if nil == err { // if not an error, is a DID
		err = d.identityCache.Delete(ctx, did.String())
		if err == cache.ErrCacheMiss {
			return nil
		}
		return err
	}
	return fmt.Errorf("at-identifier neither a Handle nor a DID")
}
