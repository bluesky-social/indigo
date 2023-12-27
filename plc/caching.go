package plc

import (
	"context"
	"time"

	"github.com/bluesky-social/indigo/did"
	arc "github.com/hashicorp/golang-lru/arc/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

type CachingDidResolver struct {
	res    did.Resolver
	maxAge time.Duration
	cache  *arc.ARCCache[string, *cachedDoc]
}

type cachedDoc struct {
	cachedAt time.Time
	doc      *did.Document
}

func NewCachingDidResolver(res did.Resolver, maxAge time.Duration, size int) *CachingDidResolver {
	c, err := arc.NewARC[string, *cachedDoc](size)
	if err != nil {
		panic(err)
	}

	return &CachingDidResolver{
		res:    res,
		cache:  c,
		maxAge: maxAge,
	}
}

func (r *CachingDidResolver) FlushCacheFor(didstr string) {
	r.cache.Remove(didstr)
}

func (r *CachingDidResolver) tryCache(did string) (*did.Document, bool) {
	cd, ok := r.cache.Get(did)
	if !ok {
		return nil, false
	}

	if time.Since(cd.cachedAt) > r.maxAge {
		return nil, false
	}

	return cd.doc, true
}

func (r *CachingDidResolver) putCache(did string, doc *did.Document) {
	r.cache.Add(did, &cachedDoc{
		doc:      doc,
		cachedAt: time.Now(),
	})
}

func (r *CachingDidResolver) GetDocument(ctx context.Context, didstr string) (*did.Document, error) {
	ctx, span := otel.Tracer("cacheResolver").Start(ctx, "getDocument")
	defer span.End()

	doc, ok := r.tryCache(didstr)
	if ok {
		span.SetAttributes(attribute.Bool("cache", true))
		cacheHitsTotal.Inc()
		return doc, nil
	}
	cacheMissesTotal.Inc()
	span.SetAttributes(attribute.Bool("cache", false))

	doc, err := r.res.GetDocument(ctx, didstr)
	if err != nil {
		return nil, err
	}

	r.putCache(didstr, doc)
	return doc, nil
}
