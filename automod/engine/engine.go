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
// NOTE: careful when initializing: several fields must not be nil or zero, even though they are pointer type.
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

func (eng *Engine) ProcessIdentityEvent(ctx context.Context, typ string, did syntax.DID) error {
	// similar to an HTTP server, we want to recover any panics from rule execution
	defer func() {
		if r := recover(); r != nil {
			eng.Logger.Error("automod event execution exception", "err", r, "did", did, "type", typ)
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
	ac := NewAccountContext(ctx, eng, *am)
	if err := eng.Rules.CallIdentityRules(&ac); err != nil {
		return err
	}
	eng.CanonicalLogLineAccount(&ac)
	eng.PurgeAccountCaches(ctx, am.Identity.DID)
	if err := eng.persistAccountModActions(&ac); err != nil {
		return err
	}
	if err := eng.persistCounters(ctx, &ac.effects); err != nil {
		return err
	}
	return nil
}

func (eng *Engine) ProcessRecordOp(ctx context.Context, op RecordOp) error {
	// similar to an HTTP server, we want to recover any panics from rule execution
	defer func() {
		if r := recover(); r != nil {
			eng.Logger.Error("automod event execution exception", "err", r, "did", op.DID, "collection", op.Collection, "rkey", op.RecordKey)
		}
	}()

	if err := op.Validate(); err != nil {
		return fmt.Errorf("bad record op: %w", err)
	}
	ident, err := eng.Directory.LookupDID(ctx, op.DID)
	if err != nil {
		return fmt.Errorf("resolving identity: %w", err)
	}
	if ident == nil {
		return fmt.Errorf("identity not found for did: %s", op.DID)
	}

	am, err := eng.GetAccountMeta(ctx, ident)
	if err != nil {
		return err
	}
	rc := NewRecordContext(ctx, eng, *am, op)
	rc.Logger.Debug("processing record")
	switch op.Action {
	case CreateOp, UpdateOp:
		if err := eng.Rules.CallRecordRules(&rc); err != nil {
			return err
		}
	case DeleteOp:
		if err := eng.Rules.CallRecordDeleteRules(&rc); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unexpected op action: %s", op.Action)
	}
	eng.CanonicalLogLineRecord(&rc)
	// purge the account meta cache when profile is updated
	if rc.RecordOp.Collection == "app.bsky.actor.profile" {
		eng.PurgeAccountCaches(ctx, am.Identity.DID)
	}
	if err := eng.persistRecordModActions(&rc); err != nil {
		return err
	}
	if err := eng.persistCounters(ctx, &rc.effects); err != nil {
		return err
	}
	return nil
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

func (e *Engine) CanonicalLogLineAccount(c *AccountContext) {
	c.Logger.Info("canonical-event-line",
		"accountLabels", c.effects.AccountLabels,
		"accountFlags", c.effects.AccountFlags,
		"accountTakedown", c.effects.AccountTakedown,
		"accountReports", len(c.effects.AccountReports),
	)
}

func (e *Engine) CanonicalLogLineRecord(c *RecordContext) {
	c.Logger.Info("canonical-event-line",
		"accountLabels", c.effects.AccountLabels,
		"accountFlags", c.effects.AccountFlags,
		"accountTakedown", c.effects.AccountTakedown,
		"accountReports", len(c.effects.AccountReports),
		"recordLabels", c.effects.RecordLabels,
		"recordFlags", c.effects.RecordFlags,
		"recordTakedown", c.effects.RecordTakedown,
		"recordReports", len(c.effects.RecordReports),
	)
}

func splitRepoPath(path string) (string, string, error) {
	parts := strings.SplitN(path, "/", 3)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid record path: %s", path)
	}
	return parts[0], parts[1], nil
}
