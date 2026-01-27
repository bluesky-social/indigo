// package group provides a way to manage the lifecycle of a group of goroutines.
package group

import (
	"context"
	"fmt"
	"sync"
)

// G manages the lifetime of a set of goroutines from a common context.
// The first goroutine in the group to return will cause the context to be canceled,
// terminating the remaining goroutines.
type G struct {
	// ctx is the context passed to all goroutines in the group.
	ctx    context.Context
	cancel context.CancelFunc
	done   sync.WaitGroup

	initOnce sync.Once

	errOnce sync.Once
	err     error
}

type Option func(*G)

// WithContext uses the provided context for the group.
func WithContext(ctx context.Context) Option {
	return func(g *G) {
		g.ctx = ctx
	}
}

// New creates a new group.
func New(opts ...Option) *G {
	g := new(G)
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// init initializes the group.
func (g *G) init() {
	if g.ctx == nil {
		g.ctx = context.Background()
	}
	g.ctx, g.cancel = context.WithCancel(g.ctx)
}

// add adds a new goroutine to the group. The goroutine should exit when the context
// passed to it is canceled.
func (g *G) Add(fn func(context.Context) error) {
	g.initOnce.Do(g.init)
	g.done.Add(1)
	go func() {
		defer g.done.Done()
		defer g.cancel()
		defer func() {
			if r := recover(); r != nil {
				g.errOnce.Do(func() {
					if err, ok := r.(error); ok {
						g.err = err
					} else {
						g.err = fmt.Errorf("panic: %v", r)
					}
				})
			}
		}()
		if err := fn(g.ctx); err != nil {
			g.errOnce.Do(func() { g.err = err })
		}
	}()
}

// Wait waits for all goroutines in the group to exit.
// If any of the goroutines fail with an error, Wait will return the first error.
func (g *G) Wait() error {
	g.done.Wait()
	g.errOnce.Do(func() {
		// noop, required to synchronise on the errOnce mutex.
	})
	return g.err
}
