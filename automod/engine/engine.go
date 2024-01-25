package engine

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod/cachestore"
	"github.com/bluesky-social/indigo/automod/countstore"
	"github.com/bluesky-social/indigo/automod/flagstore"
	"github.com/bluesky-social/indigo/automod/setstore"
	"github.com/bluesky-social/indigo/xrpc"
)

const (
	recordEventTimeout       = 20 * time.Second
	identityEventTimeout     = 10 * time.Second
	notificationEventTimeout = 5 * time.Second
)

// runtime for executing rules, managing state, and recording moderation actions.
//
// NOTE: careful when initializing: several fields must not be nil or zero, even though they are pointer type.
type Engine struct {
	Logger    *slog.Logger
	Directory identity.Directory
	Rules     RuleSet
	Counters  countstore.CountStore
	Sets      setstore.SetStore
	Cache     cachestore.CacheStore
	Flags     flagstore.FlagStore
	// unlike the other sub-modules, this field (Notifier) may be nil
	Notifier Notifier
	// use to fetch public account metadata from AppView
	BskyClient *xrpc.Client
	// used to persist moderation actions in mod service (optional)
	AdminClient *xrpc.Client
}

// Entrypoint for external code pushing arbitrary identity events in to the engine.
//
// This method can be called concurrently, though cached state may end up inconsistent if multiple events for the same account (DID) are processed in parallel.
func (eng *Engine) ProcessIdentityEvent(ctx context.Context, typ string, did syntax.DID) error {
	// similar to an HTTP server, we want to recover any panics from rule execution
	defer func() {
		if r := recover(); r != nil {
			eng.Logger.Error("automod event execution exception", "err", r, "did", did, "type", typ)
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, identityEventTimeout)
	defer cancel()

	ident, err := eng.Directory.LookupDID(ctx, did)
	if err != nil {
		return fmt.Errorf("resolving identity: %w", err)
	}
	if ident == nil {
		return fmt.Errorf("identity not found for DID: %s", did.String())
	}

	am, err := eng.GetAccountMeta(ctx, ident)
	if err != nil {
		return fmt.Errorf("failed to fetch account metadata: %w", err)
	}
	ac := NewAccountContext(ctx, eng, *am)
	if err := eng.Rules.CallIdentityRules(&ac); err != nil {
		return fmt.Errorf("rule execution failed: %w", err)
	}
	eng.CanonicalLogLineAccount(&ac)
	eng.PurgeAccountCaches(ctx, am.Identity.DID)
	if err := eng.persistAccountModActions(&ac); err != nil {
		return fmt.Errorf("failed to persist actions for identity event: %w", err)
	}
	if err := eng.persistCounters(ctx, ac.effects); err != nil {
		return fmt.Errorf("failed to persist counters for identity event: %w", err)
	}
	return nil
}

// Entrypoint for external code pushing repository updates. A simple repo commit results in multiple calls.
//
// This method can be called concurrently, though cached state may end up inconsistent if multiple events for the same account (DID) are processed in parallel.
func (eng *Engine) ProcessRecordOp(ctx context.Context, op RecordOp) error {
	// similar to an HTTP server, we want to recover any panics from rule execution
	defer func() {
		if r := recover(); r != nil {
			eng.Logger.Error("automod event execution exception", "err", r, "did", op.DID, "collection", op.Collection, "rkey", op.RecordKey)
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, recordEventTimeout)
	defer cancel()

	if err := op.Validate(); err != nil {
		return fmt.Errorf("bad record op: %w", err)
	}
	ident, err := eng.Directory.LookupDID(ctx, op.DID)
	if err != nil {
		return fmt.Errorf("resolving identity: %w", err)
	}
	if ident == nil {
		return fmt.Errorf("identity not found for DID: %s", op.DID)
	}

	am, err := eng.GetAccountMeta(ctx, ident)
	if err != nil {
		return fmt.Errorf("failed to fetch account metadata: %w", err)
	}
	rc := NewRecordContext(ctx, eng, *am, op)
	rc.Logger.Debug("processing record")
	switch op.Action {
	case CreateOp, UpdateOp:
		if err := eng.Rules.CallRecordRules(&rc); err != nil {
			return fmt.Errorf("rule execution failed: %w", err)
		}
	case DeleteOp:
		if err := eng.Rules.CallRecordDeleteRules(&rc); err != nil {
			return fmt.Errorf("rule execution failed: %w", err)
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
		return fmt.Errorf("failed to persist actions for record event: %w", err)
	}
	if err := eng.persistCounters(ctx, rc.effects); err != nil {
		return fmt.Errorf("failed to persist counts for record event: %w", err)
	}
	return nil
}

// returns a boolean indicating "block the event"
func (eng *Engine) ProcessNotificationEvent(ctx context.Context, senderDID, recipientDID syntax.DID, reason string, subject syntax.ATURI) (bool, error) {
	// similar to an HTTP server, we want to recover any panics from rule execution
	defer func() {
		if r := recover(); r != nil {
			eng.Logger.Error("automod event execution exception", "err", r, "sender", senderDID, "recipient", recipientDID)
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, notificationEventTimeout)
	defer cancel()

	senderIdent, err := eng.Directory.LookupDID(ctx, senderDID)
	if err != nil {
		return false, fmt.Errorf("resolving identity: %w", err)
	}
	if senderIdent == nil {
		return false, fmt.Errorf("identity not found for sender DID: %s", senderDID.String())
	}

	recipientIdent, err := eng.Directory.LookupDID(ctx, recipientDID)
	if err != nil {
		return false, fmt.Errorf("resolving identity: %w", err)
	}
	if recipientIdent == nil {
		return false, fmt.Errorf("identity not found for sender DID: %s", recipientDID.String())
	}

	senderMeta, err := eng.GetAccountMeta(ctx, senderIdent)
	if err != nil {
		return false, fmt.Errorf("failed to fetch account metadata: %w", err)
	}
	recipientMeta, err := eng.GetAccountMeta(ctx, recipientIdent)
	if err != nil {
		return false, fmt.Errorf("failed to fetch account metadata: %w", err)
	}

	nc := NewNotificationContext(ctx, eng, *senderMeta, *recipientMeta, reason, subject)
	if err := eng.Rules.CallNotificationRules(&nc); err != nil {
		return false, fmt.Errorf("rule execution failed: %w", err)
	}
	eng.CanonicalLogLineNotification(&nc)
	return nc.effects.RejectEvent, nil
}

// Purge metadata caches for a specific account.
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

func (e *Engine) CanonicalLogLineNotification(c *NotificationContext) {
	c.Logger.Info("canonical-event-line",
		"accountLabels", c.effects.AccountLabels,
		"accountFlags", c.effects.AccountFlags,
		"accountTakedown", c.effects.AccountTakedown,
		"accountReports", len(c.effects.AccountReports),
		"reject", c.effects.RejectEvent,
	)
}

func splitRepoPath(path string) (string, string, error) {
	parts := strings.SplitN(path, "/", 3)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid record path: %s", path)
	}
	return parts[0], parts[1], nil
}
