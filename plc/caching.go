package plc

import (
	"context"
	"time"

	lru "github.com/hashicorp/golang-lru"
	"github.com/whyrusleeping/go-did"
)

type CachingDidResolver struct {
	res    DidResolver
	maxAge time.Duration
	cache  *lru.ARCCache
}

type cachedDoc struct {
	cachedAt time.Time
	doc      *did.Document
}

func NewCachingDidResolver(res DidResolver, maxAge time.Duration, size int) *CachingDidResolver {
	c, err := lru.NewARC(size)
	if err != nil {
		panic(err)
	}

	return &CachingDidResolver{
		res:    res,
		cache:  c,
		maxAge: maxAge,
	}
}

func (r *CachingDidResolver) tryCache(did string) (*did.Document, bool) {
	v, ok := r.cache.Get(did)
	if !ok {
		return nil, false
	}

	cd := v.(*cachedDoc)
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
	doc, ok := r.tryCache(didstr)
	if ok {
		return doc, nil
	}

	doc, err := r.res.GetDocument(ctx, didstr)
	if err != nil {
		return nil, err
	}

	r.putCache(didstr, doc)
	return doc, nil
}
