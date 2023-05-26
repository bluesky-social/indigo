package did

import (
	"context"
	"fmt"

	"github.com/whyrusleeping/go-did"
)

type Resolver interface {
	GetDocument(ctx context.Context, didstr string) (*did.Document, error)
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

func (mr *MultiResolver) GetDocument(ctx context.Context, didstr string) (*did.Document, error) {
	pdid, err := did.ParseDID(didstr)
	if err != nil {
		return nil, err
	}

	method := pdid.Protocol()

	res, ok := mr.handlers[method]
	if !ok {
		return nil, fmt.Errorf("unknown did method: %q", method)
	}

	return res.GetDocument(ctx, didstr)
}
