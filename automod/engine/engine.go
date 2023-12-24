package engine

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod/cachestore"
	"github.com/bluesky-social/indigo/automod/countstore"
	"github.com/bluesky-social/indigo/automod/flagstore"
	"github.com/bluesky-social/indigo/automod/setstore"
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
	Sets        setstore.SetStore
	Cache       cachestore.CacheStore
	Flags       flagstore.FlagStore
	RelayClient *xrpc.Client
	BskyClient  *xrpc.Client
	// used to persist moderation actions in mod service (optional)
	AdminClient     *xrpc.Client
	SlackWebhookURL string
}

func (eng *Engine) ProcessIdentityEvent(ctx context.Context, t string, did syntax.DID) error {
	// similar to an HTTP server, we want to recover any panics from rule execution
	defer func() {
		if r := recover(); r != nil {
			eng.Logger.Error("automod event execution exception", "err", r, "did", did, "type", t)
		}
	}()

	ident, err := eng.Directory.LookupDID(ctx, did)
	if err != nil {
		return fmt.Errorf("resolving identity: %w", err)
	}
	if ident == nil {
		return fmt.Errorf("identity not found for did: %s", did.String())
	}

	am, err := eng.GetAccountMeta(ctx, ident)
	if err != nil {
		return err
	}
	evt := &IdentityEvent{
		RepoEvent: RepoEvent{
			Account: *am,
		},
	}
	eff := &Effects{
		// XXX: Logger: eng.Logger.With("did", am.Identity.DID),
	}
	if err := eng.Rules.CallIdentityRules(evt, eff); err != nil {
		return err
	}
	eff.CanonicalLogLine()
	eng.PurgeAccountCaches(ctx, am.Identity.DID)
	if err := eng.persistAccountEffects(ctx, &evt.RepoEvent, eff); err != nil {
		return err
	}
	if err := eng.persistCounters(ctx, eff); err != nil {
		return err
	}
	return nil
}

func (eng *Engine) ProcessRecord(ctx context.Context, did syntax.DID, path, recCID string, rec any) error {
	// similar to an HTTP server, we want to recover any panics from rule execution
	defer func() {
		if r := recover(); r != nil {
			eng.Logger.Error("automod event execution exception", "err", r, "did", did, "path", path)
		}
	}()

	ident, err := eng.Directory.LookupDID(ctx, did)
	if err != nil {
		return fmt.Errorf("resolving identity: %w", err)
	}
	if ident == nil {
		return fmt.Errorf("identity not found for did: %s", did.String())
	}

	am, err := eng.GetAccountMeta(ctx, ident)
	if err != nil {
		return err
	}
	evt, eff := eng.NewRecordProcessingContext(*am, path, recCID, rec)
	eng.Logger.Debug("processing record", "did", ident.DID, "path", path)
	if err := eng.Rules.CallRecordRules(evt, eff); err != nil {
		return err
	}
	eff.CanonicalLogLine()
	// purge the account meta cache when profile is updated
	if evt.Collection == "app.bsky.actor.profile" {
		eng.PurgeAccountCaches(ctx, am.Identity.DID)
	}
	if err := eng.persistEffectss(ctx, evt, eff); err != nil {
		return err
	}
	if err := eng.persistCounters(ctx, eff); err != nil {
		return err
	}
	return nil
}

func (eng *Engine) ProcessRecordDelete(ctx context.Context, did syntax.DID, path string) error {
	// similar to an HTTP server, we want to recover any panics from rule execution
	defer func() {
		if r := recover(); r != nil {
			eng.Logger.Error("automod event execution exception", "err", r, "did", did, "path", path)
		}
	}()

	ident, err := eng.Directory.LookupDID(ctx, did)
	if err != nil {
		return fmt.Errorf("resolving identity: %w", err)
	}
	if ident == nil {
		return fmt.Errorf("identity not found for did: %s", did.String())
	}

	am, err := eng.GetAccountMeta(ctx, ident)
	if err != nil {
		return err
	}
	evt, eff := eng.NewRecordDeleteProcessingContext(*am, path)
	eng.Logger.Debug("processing record deletion", "did", ident.DID, "path", path)
	if err := eng.Rules.CallRecordDeleteRules(evt, eff); err != nil {
		return err
	}
	eff.CanonicalLogLine()
	// purge the account meta cache when profile is updated
	if evt.Collection == "app.bsky.actor.profile" {
		eng.PurgeAccountCaches(ctx, am.Identity.DID)
	}
	/* XXX:
	if err := eng.persistAccountEffects(ctx, evt, eff); err != nil {
		return err
	}
	if err := eng.persistRecordEffects(ctx, evt, eff); err != nil {
		return err
	}
	*/
	if err := eng.persistCounters(ctx, eff); err != nil {
		return err
	}
	return nil
}

func (e *Engine) NewRecordProcessingContext(am AccountMeta, path, recCID string, rec any) (*RecordEvent, *Effects) {
	// REVIEW: Only reason for this to be a method on the engine is because it's bifrucating the logger off from there.  Should we pinch that off?
	parts := strings.SplitN(path, "/", 2)
	return &RecordEvent{
			RepoEvent: RepoEvent{
				Account: am,
			},
			Record:     rec,
			Collection: parts[0],
			RecordKey:  parts[1],
			CID:        recCID,
		}, &Effects{
			// XXX: Logger: e.Logger.With("did", am.Identity.DID, "collection", parts[0], "rkey", parts[1]),
			RecordLabels:   []string{},
			RecordFlags:    []string{},
			RecordReports:  []ModReport{},
			RecordTakedown: false,
		}
}

func (e *Engine) NewRecordDeleteProcessingContext(am AccountMeta, path string) (*RecordDeleteEvent, *Effects) {
	parts := strings.SplitN(path, "/", 2)
	return &RecordDeleteEvent{
			RepoEvent: RepoEvent{
				Account: am,
			},
			Collection: parts[0],
			RecordKey:  parts[1],
		}, &Effects{
			// XXX: Logger: e.Logger.With("did", am.Identity.DID, "collection", parts[0], "rkey", parts[1]),
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
