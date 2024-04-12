package engine

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
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
	recordEventTimeout       = 30 * time.Second
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
	// use to fetch public account metadata from AppView; no auth
	BskyClient *xrpc.Client
	// used to persist moderation actions in ozone moderation service; optional, admin auth
	OzoneClient *xrpc.Client
	// used to fetch private account metadata from PDS or entryway; optional, admin auth
	AdminClient *xrpc.Client
	// used to fetch blobs from upstream PDS instances
	BlobClient *http.Client
}

// Entrypoint for external code pushing arbitrary identity events in to the engine.
//
// This method can be called concurrently, though cached state may end up inconsistent if multiple events for the same account (DID) are processed in parallel.
func (eng *Engine) ProcessIdentityEvent(ctx context.Context, typ string, did syntax.DID) error {
	eventProcessCount.WithLabelValues("identity").Inc()
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		eventProcessDuration.WithLabelValues("identity").Observe(duration.Seconds())
	}()

	// similar to an HTTP server, we want to recover any panics from rule execution
	defer func() {
		if r := recover(); r != nil {
			eng.Logger.Error("automod event execution exception", "err", r, "did", did, "type", typ)
			eventErrorCount.WithLabelValues("identity").Inc()
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, identityEventTimeout)
	defer cancel()

	// first purge any caches; we need to re-resolve from scratch on identity updates
	if err := eng.PurgeAccountCaches(ctx, did); err != nil {
		eng.Logger.Error("failed to purge identity cache; identity rule may not run correctly", "err", err)
	}
	ident, err := eng.Directory.LookupDID(ctx, did)
	if err != nil {
		eventErrorCount.WithLabelValues("identity").Inc()
		return fmt.Errorf("resolving identity: %w", err)
	}
	if ident == nil {
		eventErrorCount.WithLabelValues("identity").Inc()
		return fmt.Errorf("identity not found for DID: %s", did.String())
	}

	am, err := eng.GetAccountMeta(ctx, ident)
	if err != nil {
		eventErrorCount.WithLabelValues("identity").Inc()
		return fmt.Errorf("failed to fetch account metadata: %w", err)
	}
	ac := NewAccountContext(ctx, eng, *am)
	if err := eng.Rules.CallIdentityRules(&ac); err != nil {
		eventErrorCount.WithLabelValues("identity").Inc()
		return fmt.Errorf("rule execution failed: %w", err)
	}
	eng.CanonicalLogLineAccount(&ac)
	if err := eng.persistAccountModActions(&ac); err != nil {
		eventErrorCount.WithLabelValues("identity").Inc()
		return fmt.Errorf("failed to persist actions for identity event: %w", err)
	}
	if err := eng.persistCounters(ctx, ac.effects); err != nil {
		eventErrorCount.WithLabelValues("identity").Inc()
		return fmt.Errorf("failed to persist counters for identity event: %w", err)
	}
	return nil
}

// Entrypoint for external code pushing repository updates. A simple repo commit results in multiple calls.
//
// This method can be called concurrently, though cached state may end up inconsistent if multiple events for the same account (DID) are processed in parallel.
func (eng *Engine) ProcessRecordOp(ctx context.Context, op RecordOp) error {
	eventProcessCount.WithLabelValues("record").Inc()
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		eventProcessDuration.WithLabelValues("record").Observe(duration.Seconds())
	}()

	// similar to an HTTP server, we want to recover any panics from rule execution
	defer func() {
		if r := recover(); r != nil {
			eng.Logger.Error("automod event execution exception", "err", r, "did", op.DID, "collection", op.Collection, "rkey", op.RecordKey)
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, recordEventTimeout)
	defer cancel()

	if err := op.Validate(); err != nil {
		eventErrorCount.WithLabelValues("record").Inc()
		return fmt.Errorf("bad record op: %w", err)
	}
	ident, err := eng.Directory.LookupDID(ctx, op.DID)
	if err != nil {
		eventErrorCount.WithLabelValues("record").Inc()
		return fmt.Errorf("resolving identity: %w", err)
	}
	if ident == nil {
		eventErrorCount.WithLabelValues("record").Inc()
		return fmt.Errorf("identity not found for DID: %s", op.DID)
	}

	am, err := eng.GetAccountMeta(ctx, ident)
	if err != nil {
		eventErrorCount.WithLabelValues("record").Inc()
		return fmt.Errorf("failed to fetch account metadata: %w", err)
	}
	rc := NewRecordContext(ctx, eng, *am, op)
	rc.Logger.Debug("processing record")
	switch op.Action {
	case CreateOp, UpdateOp:
		if err := eng.Rules.CallRecordRules(&rc); err != nil {
			eventErrorCount.WithLabelValues("record").Inc()
			return fmt.Errorf("rule execution failed: %w", err)
		}
	case DeleteOp:
		if err := eng.Rules.CallRecordDeleteRules(&rc); err != nil {
			eventErrorCount.WithLabelValues("record").Inc()
			return fmt.Errorf("rule execution failed: %w", err)
		}
	default:
		eventErrorCount.WithLabelValues("record").Inc()
		return fmt.Errorf("unexpected op action: %s", op.Action)
	}
	eng.CanonicalLogLineRecord(&rc)
	// purge the account meta cache when profile is updated
	if rc.RecordOp.Collection == "app.bsky.actor.profile" {
		if err := eng.PurgeAccountCaches(ctx, op.DID); err != nil {
			eng.Logger.Error("failed to purge identity cache", "err", err)
		}
	}
	if err := eng.persistRecordModActions(&rc); err != nil {
		eventErrorCount.WithLabelValues("record").Inc()
		return fmt.Errorf("failed to persist actions for record event: %w", err)
	}
	if err := eng.persistCounters(ctx, rc.effects); err != nil {
		eventErrorCount.WithLabelValues("record").Inc()
		return fmt.Errorf("failed to persist counts for record event: %w", err)
	}
	return nil
}

// returns a boolean indicating "block the event"
func (eng *Engine) ProcessNotificationEvent(ctx context.Context, senderDID, recipientDID syntax.DID, reason string, subject syntax.ATURI) (bool, error) {
	eventProcessCount.WithLabelValues("notif").Inc()
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		eventProcessDuration.WithLabelValues("notif").Observe(duration.Seconds())
	}()

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
		eventErrorCount.WithLabelValues("notif").Inc()
		return false, fmt.Errorf("resolving identity: %w", err)
	}
	if senderIdent == nil {
		eventErrorCount.WithLabelValues("notif").Inc()
		return false, fmt.Errorf("identity not found for sender DID: %s", senderDID.String())
	}

	recipientIdent, err := eng.Directory.LookupDID(ctx, recipientDID)
	if err != nil {
		eventErrorCount.WithLabelValues("notif").Inc()
		return false, fmt.Errorf("resolving identity: %w", err)
	}
	if recipientIdent == nil {
		eventErrorCount.WithLabelValues("notif").Inc()
		return false, fmt.Errorf("identity not found for sender DID: %s", recipientDID.String())
	}

	senderMeta, err := eng.GetAccountMeta(ctx, senderIdent)
	if err != nil {
		eventErrorCount.WithLabelValues("notif").Inc()
		return false, fmt.Errorf("failed to fetch account metadata: %w", err)
	}
	recipientMeta, err := eng.GetAccountMeta(ctx, recipientIdent)
	if err != nil {
		eventErrorCount.WithLabelValues("notif").Inc()
		return false, fmt.Errorf("failed to fetch account metadata: %w", err)
	}

	nc := NewNotificationContext(ctx, eng, *senderMeta, *recipientMeta, reason, subject)
	if err := eng.Rules.CallNotificationRules(&nc); err != nil {
		eventErrorCount.WithLabelValues("notif").Inc()
		return false, fmt.Errorf("rule execution failed: %w", err)
	}
	eng.CanonicalLogLineNotification(&nc)
	return nc.effects.RejectEvent, nil
}

// Purge metadata caches for a specific account.
func (e *Engine) PurgeAccountCaches(ctx context.Context, did syntax.DID) error {
	e.Logger.Debug("purging account caches", "did", did.String())
	dirErr := e.Directory.Purge(ctx, did.AtIdentifier())
	cacheErr := e.Cache.Purge(ctx, "acct", did.String())
	if dirErr != nil {
		return dirErr
	}
	return cacheErr
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
