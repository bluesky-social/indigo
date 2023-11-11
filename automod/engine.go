package automod

import (
	"context"
	"log/slog"
	"sync"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/xrpc"
)

// runtime for executing rules, managing state, and recording moderation actions
type Engine struct {
	// current rule sets. will eventually be possible to swap these out at runtime
	RulesMap  sync.Map
	Directory identity.Directory
	// used to persist moderation actions in mod service (optional)
	AdminClient *xrpc.Client
	CountStore  CountStore
}

func (e *Engine) ExecuteIdentity() error {
	ctx := context.Background()

	// similar to an HTTP server, we want to recover any panics from rule execution
	defer func() {
		if r := recover(); r != nil {
			slog.Error("automod event execution exception", "err", r)
			// TODO: mark repo as dirty?
			// TODO: circuit-break on repeated panics?
		}
	}()

	_ = ctx
	return nil
}

func (e *Engine) ExecuteCommit() error {
	ctx := context.Background()

	// similar to an HTTP server, we want to recover any panics from rule execution
	defer func() {
		if r := recover(); r != nil {
			slog.Error("automod event execution exception", "err", r)
			// TODO: mark repo as dirty?
			// TODO: circuit-break on repeated panics?
		}
	}()

	_ = ctx
	return nil
}

func (e *Engine) PersistModActions() error {
	// XXX
	return nil
}

func (e *Engine) GetCount(key, period string) (int, error) {
	return e.CountStore.GetCount(context.TODO(), key, period)
}

func (e *Engine) InSet(name, val string) (bool, error) {
	// XXX: implement
	return false, nil
}
