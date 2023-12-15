package automod

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod/countstore"
	"github.com/bluesky-social/indigo/xrpc"
)

// runtime for executing rules, managing state, and recording moderation actions.
//
// TODO: careful when initializing: several fields should not be null or zero, even though they are pointer type.
type Engine struct {
	Logger      *slog.Logger
	Directory   identity.Directory
	Rules       RuleSet
	Counters    countstore.CountStore
	Sets        SetStore
	Cache       CacheStore
	Flags       FlagStore
	RelayClient *xrpc.Client
	BskyClient  *xrpc.Client
	// used to persist moderation actions in mod service (optional)
	AdminClient     *xrpc.Client
	SlackWebhookURL string
}

func (e *Engine) ProcessIdentityEvent(ctx context.Context, t string, did syntax.DID) error {
	// similar to an HTTP server, we want to recover any panics from rule execution
	defer func() {
		if r := recover(); r != nil {
			e.Logger.Error("automod event execution exception", "err", r, "did", did, "type", t)
		}
	}()

	ident, err := e.Directory.LookupDID(ctx, did)
	if err != nil {
		return fmt.Errorf("resolving identity: %w", err)
	}
	if ident == nil {
		return fmt.Errorf("identity not found for did: %s", did.String())
	}

	am, err := e.GetAccountMeta(ctx, ident)
	if err != nil {
		return err
	}
	evt := IdentityEvent{
		RepoEvent{
			Engine:  e,
			Logger:  e.Logger.With("did", am.Identity.DID),
			Account: *am,
		},
	}
	if err := e.Rules.CallIdentityRules(&evt); err != nil {
		return err
	}
	if evt.Err != nil {
		return evt.Err
	}
	evt.CanonicalLogLine()
	e.PurgeAccountCaches(ctx, am.Identity.DID)
	if err := evt.PersistActions(ctx); err != nil {
		return err
	}
	if err := evt.PersistCounters(ctx); err != nil {
		return err
	}
	// check for any new errors during persist
	if evt.Err != nil {
		return evt.Err
	}
	return nil
}

func (e *Engine) ProcessRecord(ctx context.Context, did syntax.DID, path, recCID string, rec any) error {
	// similar to an HTTP server, we want to recover any panics from rule execution
	defer func() {
		if r := recover(); r != nil {
			e.Logger.Error("automod event execution exception", "err", r, "did", did, "path", path)
		}
	}()

	ident, err := e.Directory.LookupDID(ctx, did)
	if err != nil {
		return fmt.Errorf("resolving identity: %w", err)
	}
	if ident == nil {
		return fmt.Errorf("identity not found for did: %s", did.String())
	}

	am, err := e.GetAccountMeta(ctx, ident)
	if err != nil {
		return err
	}
	evt := e.NewRecordEvent(*am, path, recCID, rec)
	e.Logger.Debug("processing record", "did", ident.DID, "path", path)
	if err := e.Rules.CallRecordRules(&evt); err != nil {
		return err
	}
	if evt.Err != nil {
		return evt.Err
	}
	evt.CanonicalLogLine()
	// purge the account meta cache when profile is updated
	if evt.Collection == "app.bsky.actor.profile" {
		e.PurgeAccountCaches(ctx, am.Identity.DID)
	}
	if err := evt.PersistActions(ctx); err != nil {
		return err
	}
	if err := evt.PersistCounters(ctx); err != nil {
		return err
	}
	// check for any new errors during persist
	if evt.Err != nil {
		return evt.Err
	}
	return nil
}

func (e *Engine) ProcessRecordDelete(ctx context.Context, did syntax.DID, path string) error {
	// similar to an HTTP server, we want to recover any panics from rule execution
	defer func() {
		if r := recover(); r != nil {
			e.Logger.Error("automod event execution exception", "err", r, "did", did, "path", path)
		}
	}()

	ident, err := e.Directory.LookupDID(ctx, did)
	if err != nil {
		return fmt.Errorf("resolving identity: %w", err)
	}
	if ident == nil {
		return fmt.Errorf("identity not found for did: %s", did.String())
	}

	am, err := e.GetAccountMeta(ctx, ident)
	if err != nil {
		return err
	}
	evt := e.NewRecordDeleteEvent(*am, path)
	e.Logger.Debug("processing record deletion", "did", ident.DID, "path", path)
	if err := e.Rules.CallRecordDeleteRules(&evt); err != nil {
		return err
	}
	if evt.Err != nil {
		return evt.Err
	}
	evt.CanonicalLogLine()
	// purge the account meta cache when profile is updated
	if evt.Collection == "app.bsky.actor.profile" {
		e.PurgeAccountCaches(ctx, am.Identity.DID)
	}
	if err := evt.PersistActions(ctx); err != nil {
		return err
	}
	if err := evt.PersistCounters(ctx); err != nil {
		return err
	}
	return nil
}

func (e *Engine) NewRecordEvent(am AccountMeta, path, recCID string, rec any) RecordEvent {
	parts := strings.SplitN(path, "/", 2)
	return RecordEvent{
		RepoEvent{
			Engine:  e,
			Logger:  e.Logger.With("did", am.Identity.DID, "collection", parts[0], "rkey", parts[1]),
			Account: am,
		},
		rec,
		parts[0],
		parts[1],
		recCID,
		[]string{},
		false,
		[]ModReport{},
		[]string{},
	}
}

func (e *Engine) NewRecordDeleteEvent(am AccountMeta, path string) RecordDeleteEvent {
	parts := strings.SplitN(path, "/", 2)
	return RecordDeleteEvent{
		RepoEvent{
			Engine:  e,
			Logger:  e.Logger.With("did", am.Identity.DID, "collection", parts[0], "rkey", parts[1]),
			Account: am,
		},
		parts[0],
		parts[1],
	}
}

func (e *Engine) GetCount(name, val, period string) (int, error) {
	return e.Counters.GetCount(context.TODO(), name, val, period)
}

func (e *Engine) GetCountDistinct(name, bucket, period string) (int, error) {
	return e.Counters.GetCountDistinct(context.TODO(), name, bucket, period)
}

// checks if `val` is an element of set `name`
func (e *Engine) InSet(name, val string) (bool, error) {
	return e.Sets.InSet(context.TODO(), name, val)
}

// purge caches of any exiting metadata
func (e *Engine) PurgeAccountCaches(ctx context.Context, did syntax.DID) error {
	e.Directory.Purge(ctx, did.AtIdentifier())
	return e.Cache.Purge(ctx, "acct", did.String())
}
