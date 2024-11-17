package did

import (
	"context"
	"fmt"
	"time"

	"github.com/whyrusleeping/go-did"
)

type Resolver interface {
	GetDocument(ctx context.Context, didstr string) (*did.Document, error)
	FlushCacheFor(did string)
}

type MultiResolver struct {
	handlers map[string]Resolver
}

func NewMultiResolver() *MultiResolver {
	return &MultiResolver{
		handlers: make(map[string]Resolver),
	}
}

func (mr *MultiResolver) AddHandler(method string, res Resolver) {
	mr.handlers[method] = res
}

func (mr *MultiResolver) FlushCacheFor(didstr string) {
	pdid, err := did.ParseDID(didstr)
	if err != nil {
		return
	}

	method := pdid.Protocol()

	res, ok := mr.handlers[method]
	if !ok {
		return
	}

	res.FlushCacheFor(didstr)
}

func (mr *MultiResolver) GetDocument(ctx context.Context, didstr string) (*did.Document, error) {
	s := time.Now()

	pdid, err := did.ParseDID(didstr)
	if err != nil {
		return nil, err
	}

	method := pdid.Protocol()
	defer func() {
		mrResolveDuration.WithLabelValues(method).Observe(time.Since(s).Seconds())
	}()

	res, ok := mr.handlers[method]
	if !ok {
		return nil, fmt.Errorf("unknown did method: %q", method)
	}

	mrResolvedDidsTotal.WithLabelValues(method).Inc()

	return res.GetDocument(ctx, didstr)
}
