package automod

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/go-redis/cache/v9"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/redis/go-redis/v9"
)

var redisDirPrefix string = "dir/"

// uses redis as a cache for identity lookups. includes a local cache layer as well, for hot keys
type RedisDirectory struct {
	Inner  identity.Directory
	ErrTTL time.Duration
	HitTTL time.Duration

	handleCache       *cache.Cache
	identityCache     *cache.Cache
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
	Identity *identity.Identity
	Err      error
}

var _ identity.Directory = (*RedisDirectory)(nil)

func NewRedisDirectory(inner identity.Directory, redisURL string, hitTTL, errTTL time.Duration) (*RedisDirectory, error) {
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
		LocalCache: cache.NewTinyLFU(10_000, hitTTL),
	})
	identityCache := cache.New(&cache.Options{
		Redis:      rdb,
		LocalCache: cache.NewTinyLFU(10_000, hitTTL),
	})
	return &RedisDirectory{
		Inner:         inner,
		ErrTTL:        errTTL,
		HitTTL:        hitTTL,
		handleCache:   handleCache,
		identityCache: identityCache,
	}, nil
}

func (d *RedisDirectory) IsHandleStale(e *HandleEntry) bool {
	if e.Err != nil && time.Since(e.Updated) > d.ErrTTL {
		return true
	}
	return false
}

func (d *RedisDirectory) IsIdentityStale(e *IdentityEntry) bool {
	if e.Err != nil && time.Since(e.Updated) > d.ErrTTL {
		return true
	}
	return false
}

func (d *RedisDirectory) updateHandle(ctx context.Context, h syntax.Handle) (*HandleEntry, error) {
	ident, err := d.Inner.LookupHandle(ctx, h)
	if err != nil {
		he := HandleEntry{
			Updated: time.Now(),
			DID:     "",
			Err:     err,
		}
		d.handleCache.Set(&cache.Item{
			Ctx:   ctx,
			Key:   redisDirPrefix + h.String(),
			Value: he,
			TTL:   d.ErrTTL,
		})
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

	d.identityCache.Set(&cache.Item{
		Ctx:   ctx,
		Key:   redisDirPrefix + ident.DID.String(),
		Value: entry,
		TTL:   d.HitTTL,
	})
	d.handleCache.Set(&cache.Item{
		Ctx:   ctx,
		Key:   redisDirPrefix + h.String(),
		Value: he,
		TTL:   d.HitTTL,
	})
	return &he, nil
}

func (d *RedisDirectory) ResolveHandle(ctx context.Context, h syntax.Handle) (syntax.DID, error) {
	var entry HandleEntry
	err := d.handleCache.Get(ctx, redisDirPrefix+h.String(), &entry)
	if err != nil && err != cache.ErrCacheMiss {
		return "", err
	}
	if err != cache.ErrCacheMiss && !d.IsHandleStale(&entry) {
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
			if err != cache.ErrCacheMiss && !d.IsHandleStale(&entry) {
				return entry.DID, entry.Err
			}
			return "", fmt.Errorf("identity not found in cache after coalesce returned")
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	var did syntax.DID
	// Update the Handle Entry from PLC and cache the result
	newEntry, err := d.updateHandle(ctx, h)
	if err == nil && newEntry != nil {
		did = newEntry.DID
	}
	// Cleanup the coalesce map and close the results channel
	d.handleLookupChans.Delete(h.String())
	// Callers waiting will now get the result from the cache
	close(res)

	return did, err
}

func (d *RedisDirectory) updateDID(ctx context.Context, did syntax.DID) (*IdentityEntry, error) {
	ident, err := d.Inner.LookupDID(ctx, did)
	// wipe parsed public key; it's a waste of space
	if nil == err {
		ident.ParsedPublicKey = nil
	}
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

	d.identityCache.Set(&cache.Item{
		Ctx:   ctx,
		Key:   redisDirPrefix + did.String(),
		Value: entry,
		TTL:   d.HitTTL,
	})
	if he != nil {
		d.handleCache.Set(&cache.Item{
			Ctx:   ctx,
			Key:   redisDirPrefix + ident.Handle.String(),
			Value: *he,
			TTL:   d.HitTTL,
		})
	}
	return &entry, nil
}

func (d *RedisDirectory) LookupDID(ctx context.Context, did syntax.DID) (*identity.Identity, error) {
	var entry IdentityEntry
	err := d.identityCache.Get(ctx, redisDirPrefix+did.String(), &entry)
	if err != nil && err != cache.ErrCacheMiss {
		return nil, err
	}
	if err != cache.ErrCacheMiss && !d.IsIdentityStale(&entry) {
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
			if err != cache.ErrCacheMiss && !d.IsIdentityStale(&entry) {
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
		return d.handleCache.Delete(ctx, handle.String())
	}
	did, err := a.AsDID()
	if nil == err { // if not an error, is a DID
		return d.identityCache.Delete(ctx, did.String())
	}
	return fmt.Errorf("at-identifier neither a Handle nor a DID")
}

var handleCacheHits = promauto.NewCounter(prometheus.CounterOpts{
	Name: "atproto_redis_directory_handle_cache_hits",
	Help: "Number of cache hits for ATProto handle lookups",
})

var handleCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
	Name: "atproto_redis_directory_handle_cache_misses",
	Help: "Number of cache misses for ATProto handle lookups",
})

var identityCacheHits = promauto.NewCounter(prometheus.CounterOpts{
	Name: "atproto_redis_directory_identity_cache_hits",
	Help: "Number of cache hits for ATProto identity lookups",
})

var identityCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
	Name: "atproto_redis_directory_identity_cache_misses",
	Help: "Number of cache misses for ATProto identity lookups",
})

var identityRequestsCoalesced = promauto.NewCounter(prometheus.CounterOpts{
	Name: "atproto_redis_directory_identity_requests_coalesced",
	Help: "Number of identity requests coalesced",
})

var handleRequestsCoalesced = promauto.NewCounter(prometheus.CounterOpts{
	Name: "atproto_redis_directory_handle_requests_coalesced",
	Help: "Number of handle requests coalesced",
})
