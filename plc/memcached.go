package plc

import (
	"context"
	"encoding/json"
	"github.com/bradfitz/gomemcache/memcache"
	"go.opentelemetry.io/otel/attribute"
	"time"

	"github.com/bluesky-social/indigo/did"
	"go.opentelemetry.io/otel"
)

type MemcachedDidResolver struct {
	mcd    *memcache.Client
	res    did.Resolver
	maxAge int32
}

func NewMemcachedDidResolver(res did.Resolver, maxAge time.Duration, servers []string) *MemcachedDidResolver {
	expiry := int32(0)
	if maxAge.Seconds() > (30 * 24 * 60 * 60) {
		// clamp expiry at 30 days minus a minute for memcached
		expiry = (30 * 24 * 60 * 60) - 60
	} else {
		expiry = int32(maxAge.Seconds())
	}
	client := memcache.New(servers...)
	return &MemcachedDidResolver{
		mcd:    client,
		res:    res,
		maxAge: expiry,
	}
}

func (r *MemcachedDidResolver) FlushCacheFor(didstr string) {
	r.mcd.Delete(didstr)
	r.res.FlushCacheFor(didstr)
}

func (r *MemcachedDidResolver) tryCache(didstr string) (*did.Document, bool) {
	ob, err := r.mcd.Get(didstr)
	if (ob == nil) || (err != nil) {
		return nil, false
	}
	var doc did.Document
	err = json.Unmarshal(ob.Value, &doc)
	if err != nil {
		// TODO: log error?
		return nil, false
	}

	return &doc, true
}

func (r *MemcachedDidResolver) putCache(did string, doc *did.Document) {
	blob, err := json.Marshal(doc)
	if err != nil {
		// TODO: log error
		return
	}
	item := memcache.Item{
		Key:        did,
		Value:      blob,
		Expiration: int32(r.maxAge),
	}
	r.mcd.Set(&item)
}

func (r *MemcachedDidResolver) GetDocument(ctx context.Context, didstr string) (*did.Document, error) {
	ctx, span := otel.Tracer("cacheResolver").Start(ctx, "getDocument")
	defer span.End()

	doc, ok := r.tryCache(didstr)
	if ok {
		span.SetAttributes(attribute.Bool("cache", true))
		memcacheHitsTotal.Inc()
		return doc, nil
	}
	memcacheMissesTotal.Inc()
	span.SetAttributes(attribute.Bool("cache", false))

	doc, err := r.res.GetDocument(ctx, didstr)
	if err != nil {
		return nil, err
	}

	r.putCache(didstr, doc)
	return doc, nil
}
