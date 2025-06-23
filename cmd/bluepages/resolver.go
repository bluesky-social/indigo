package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/go-redis/cache/v9"
	"github.com/redis/go-redis/v9"
)

// This file is a fork of indigo:atproto/identity/redisdir. It stores raw DID documents, not identities, and implements `identity.Resolver`.

// Uses redis as a cache for identity lookups.
//
// Includes an in-process LRU cache as well (provided by the redis client library), for hot key (identities).
type RedisResolver struct {
	Inner            identity.Resolver
	ErrTTL           time.Duration
	HitTTL           time.Duration
	InvalidHandleTTL time.Duration
	Logger           *slog.Logger

	handleCache        *cache.Cache
	didCache           *cache.Cache
	didResolveChans    sync.Map
	handleResolveChans sync.Map
}

type handleEntry struct {
	Updated time.Time
	// needs to be pointer type, because unmarshalling empty string would be an error
	DID *syntax.DID
	Err error
}

type didEntry struct {
	Updated time.Time
	RawDoc  json.RawMessage
	Err     error
}

var _ identity.Resolver = (*RedisResolver)(nil)

// Creates a new caching `identity.Resolver` wrapper around an existing directory, using Redis and in-process LRU for caching.
//
// `redisURL` contains all the redis connection config options.
// `hitTTL` and `errTTL` define how long successful and errored identity metadata should be cached (respectively). errTTL is expected to be shorted than hitTTL.
// `lruSize` is the size of the in-process cache, for each of the handle and identity caches. 10000 is a reasonable default.
//
// NOTE: Errors returned may be inconsistent with the base directory, or between calls. This is because cached errors are serialized/deserialized and that may break equality checks.
func NewRedisResolver(inner identity.Resolver, redisURL string, hitTTL, errTTL, invalidHandleTTL time.Duration, lruSize int) (*RedisResolver, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("could not configure redis identity cache: %w", err)
	}
	rdb := redis.NewClient(opt)
	// check redis connection
	_, err = rdb.Ping(context.TODO()).Result()
	if err != nil {
		return nil, fmt.Errorf("could not connect to redis identity cache: %w", err)
	}
	handleCache := cache.New(&cache.Options{
		Redis:      rdb,
		LocalCache: cache.NewTinyLFU(lruSize, hitTTL),
	})
	didCache := cache.New(&cache.Options{
		Redis:      rdb,
		LocalCache: cache.NewTinyLFU(lruSize, hitTTL),
	})
	return &RedisResolver{
		Inner:            inner,
		ErrTTL:           errTTL,
		HitTTL:           hitTTL,
		InvalidHandleTTL: invalidHandleTTL,
		handleCache:      handleCache,
		didCache:         didCache,
	}, nil
}

func (d *RedisResolver) isHandleStale(e *handleEntry) bool {
	if e.Err != nil && time.Since(e.Updated) > d.ErrTTL {
		return true
	}
	return false
}

func (d *RedisResolver) isDIDStale(e *didEntry) bool {
	if e.Err != nil && time.Since(e.Updated) > d.ErrTTL {
		return true
	}
	return false
}

func (d *RedisResolver) refreshHandle(ctx context.Context, h syntax.Handle) handleEntry {
	start := time.Now()
	did, err := d.Inner.ResolveHandle(ctx, h)
	duration := time.Since(start)

	segment := "default"
	if strings.HasSuffix(segment, ".bsky.social") {
		segment = "bskysocial"
	}

	if err != nil {
		d.Logger.Info("handle resolution failed", "handle", h, "duration", duration.String(), "err", err)
		handleResolution.WithLabelValues("bluepages", "error").Inc()
		handleResolutionDuration.WithLabelValues("bluepages", "error").Observe(time.Since(start).Seconds())
		handleExternalResolutionDuration.WithLabelValues(segment, "error").Observe(time.Since(start).Seconds())
	} else {
		handleResolution.WithLabelValues("bluepages", "success").Inc()
		handleResolutionDuration.WithLabelValues("bluepages", "success").Observe(time.Since(start).Seconds())
		handleExternalResolutionDuration.WithLabelValues(segment, "success").Observe(time.Since(start).Seconds())
	}
	if duration.Seconds() > 5.0 {
		d.Logger.Info("slow handle resolution", "handle", h, "duration", duration.String())
	}

	he := handleEntry{
		Updated: time.Now(),
		Err:     err,
	}
	if did != "" {
		he.DID = &did
	}
	err = d.handleCache.Set(&cache.Item{
		Ctx:   ctx,
		Key:   "bluepages/handle/" + h.String(),
		Value: he,
		TTL:   d.ErrTTL,
	})
	if err != nil {
		d.Logger.Error("identity cache write failed", "cache", "handle", "err", err)
	}
	return he
}

func (d *RedisResolver) refreshDID(ctx context.Context, did syntax.DID) didEntry {
	method := did.Method()
	start := time.Now()
	rawDoc, err := d.Inner.ResolveDIDRaw(ctx, did)
	duration := time.Since(start)

	if err != nil {
		d.Logger.Info("DID resolution failed", "did", did, "duration", duration.String(), "err", err)
		didResolution.WithLabelValues("bluepages", "error").Inc()
		didResolutionDuration.WithLabelValues("bluepages", "error").Observe(time.Since(start).Seconds())
		didExternalResolutionDuration.WithLabelValues(method, "error").Observe(time.Since(start).Seconds())
	} else {
		didResolution.WithLabelValues("bluepages", "success").Inc()
		didResolutionDuration.WithLabelValues("bluepages", "success").Observe(time.Since(start).Seconds())
		didExternalResolutionDuration.WithLabelValues(method, "success").Observe(time.Since(start).Seconds())
	}
	if duration.Seconds() > 5.0 {
		d.Logger.Info("slow DID resolution", "did", did, "duration", duration.String(), "method", method)
	}

	// persist the DID lookup error, instead of processing it immediately
	entry := didEntry{
		Updated: time.Now(),
		RawDoc:  rawDoc,
		Err:     err,
	}

	err = d.didCache.Set(&cache.Item{
		Ctx:   ctx,
		Key:   "bluepages/did/" + did.String(),
		Value: entry,
		TTL:   d.HitTTL,
	})
	if err != nil {
		d.Logger.Error("DID cache write failed", "cache", "did", "did", did, "err", err)
	}
	return entry
}

func (d *RedisResolver) ResolveHandle(ctx context.Context, h syntax.Handle) (syntax.DID, error) {
	if h.IsInvalidHandle() {
		return "", fmt.Errorf("can not resolve handle: %w", identity.ErrInvalidHandle)
	}
	h = h.Normalize()
	var entry handleEntry
	err := d.handleCache.Get(ctx, "bluepages/handle/"+h.String(), &entry)
	if err != nil && err != cache.ErrCacheMiss {
		return "", fmt.Errorf("identity cache read failed: %w", err)
	}
	if err == nil && !d.isHandleStale(&entry) { // if no error...
		handleResolution.WithLabelValues("bluepages", "cached").Inc()
		if entry.Err != nil {
			return "", entry.Err
		} else if entry.DID != nil {
			return *entry.DID, nil
		} else {
			return "", errors.New("code flow error in redis identity directory")
		}
	}

	// Coalesce multiple requests for the same Handle
	res := make(chan struct{})
	val, loaded := d.handleResolveChans.LoadOrStore(h.String(), res)
	if loaded {
		handleResolution.WithLabelValues("bluepages", "coalesced").Inc()
		// Wait for the result from the pending request
		select {
		case <-val.(chan struct{}):
			// The result should now be in the cache
			err := d.handleCache.Get(ctx, "bluepages/handle/"+h.String(), entry)
			if err != nil && err != cache.ErrCacheMiss {
				return "", fmt.Errorf("identity cache read failed: %w", err)
			}
			if err == nil && !d.isHandleStale(&entry) { // if no error...
				if entry.Err != nil {
					return "", entry.Err
				} else if entry.DID != nil {
					return *entry.DID, nil
				} else {
					return "", errors.New("code flow error in redis identity directory")
				}
			}
			return "", errors.New("identity not found in cache after coalesce returned")
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	// Update the Handle Entry from PLC and cache the result
	newEntry := d.refreshHandle(ctx, h)

	// Cleanup the coalesce map and close the results channel
	d.handleResolveChans.Delete(h.String())
	// Callers waiting will now get the result from the cache
	close(res)

	if newEntry.Err != nil {
		return "", newEntry.Err
	}
	if newEntry.DID != nil {
		return *newEntry.DID, nil
	}
	return "", errors.New("unexpected control-flow error")
}

func (d *RedisResolver) ResolveDIDRaw(ctx context.Context, did syntax.DID) (json.RawMessage, error) {
	var entry didEntry
	err := d.didCache.Get(ctx, "bluepages/did/"+did.String(), &entry)
	if err != nil && err != cache.ErrCacheMiss {
		return nil, fmt.Errorf("DID cache read failed: %w", err)
	}
	if err == nil && !d.isDIDStale(&entry) { // if no error...
		didResolution.WithLabelValues("bluepages", "cached").Inc()
		return entry.RawDoc, entry.Err
	}

	// Coalesce multiple requests for the same DID
	res := make(chan struct{})
	val, loaded := d.didResolveChans.LoadOrStore(did.String(), res)
	if loaded {
		didResolution.WithLabelValues("bluepages", "coalesced").Inc()
		// Wait for the result from the pending request
		select {
		case <-val.(chan struct{}):
			// The result should now be in the cache
			err = d.didCache.Get(ctx, "bluepages/did/"+did.String(), &entry)
			if err != nil && err != cache.ErrCacheMiss {
				return nil, fmt.Errorf("DID cache read failed: %w", err)
			}
			if err == nil && !d.isDIDStale(&entry) { // if no error...
				return entry.RawDoc, entry.Err
			}
			return nil, errors.New("DID not found in cache after coalesce returned")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// Update the DID Entry and cache the result
	newEntry := d.refreshDID(ctx, did)

	// Cleanup the coalesce map and close the results channel
	d.didResolveChans.Delete(did.String())
	// Callers waiting will now get the result from the cache
	close(res)

	if newEntry.Err != nil {
		return nil, newEntry.Err
	}
	if newEntry.RawDoc != nil {
		return newEntry.RawDoc, nil
	}
	return nil, errors.New("unexpected control-flow error")
}

func (d *RedisResolver) ResolveDID(ctx context.Context, did syntax.DID) (*identity.DIDDocument, error) {
	b, err := d.ResolveDIDRaw(ctx, did)
	if err != nil {
		return nil, err
	}

	var doc identity.DIDDocument
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("%w: JSON DID document parse: %w", identity.ErrDIDResolutionFailed, err)
	}
	if doc.DID != did {
		return nil, fmt.Errorf("document ID did not match DID")
	}
	return &doc, nil
}

func (d *RedisResolver) PurgeHandle(ctx context.Context, handle syntax.Handle) error {
	handle = handle.Normalize()
	err := d.handleCache.Delete(ctx, "bluepages/handle/"+handle.String())
	if err == cache.ErrCacheMiss {
		return nil
	}
	return err
}

func (d *RedisResolver) PurgeDID(ctx context.Context, did syntax.DID) error {
	err := d.didCache.Delete(ctx, "bluepages/did/"+did.String())
	if err == cache.ErrCacheMiss {
		return nil
	}
	return err
}
