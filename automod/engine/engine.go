package engine

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
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

	// internal configuration
	Config EngineConfig
}

type EngineConfig struct {
	// if enabled, account metadata is not hydrated for every event by default
	SkipAccountMeta bool
	// if true, sent firehose identity and account events to ozone backend as events
	PersistSubjectHistoryOzone bool
	// time period within which automod will not re-report an account for the same reasonType
	ReportDupePeriod time.Duration
	// number of reports automod can file per day, for all subjects and types combined (circuit breaker)
	QuotaModReportDay int
	// number of takedowns automod can action per day, for all subjects combined (circuit breaker)
	QuotaModTakedownDay int
	// number of misc actions automod can do per day, for all subjects combined (circuit breaker)
	QuotaModActionDay int
}

// Entrypoint for external code pushing #identity events in to the engine.
//
// This method can be called concurrently, though cached state may end up inconsistent if multiple events for the same account (DID) are processed in parallel.
func (eng *Engine) ProcessIdentityEvent(ctx context.Context, evt comatproto.SyncSubscribeRepos_Identity) error {
	eventProcessCount.WithLabelValues("identity").Inc()
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		eventProcessDuration.WithLabelValues("identity").Observe(duration.Seconds())
	}()

	did, err := syntax.ParseDID(evt.Did)
	if err != nil {
		return fmt.Errorf("bad DID in repo #identity event (%s): %w", evt.Did, err)
	}

	// similar to an HTTP server, we want to recover any panics from rule execution
	defer func() {
		if r := recover(); r != nil {
			eng.Logger.Error("automod event execution exception", "err", r, "did", did, "type", "identity")
			eventErrorCount.WithLabelValues("identity").Inc()
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, identityEventTimeout)
	defer cancel()

	// first purge any caches; we need to re-resolve from scratch on identity updates
	if err := eng.PurgeAccountCaches(ctx, did); err != nil {
		eng.Logger.Error("failed to purge identity cache; identity rule may not run correctly", "err", err)
	}
	// TODO(bnewbold): if it was a tombstone, this might fail
	ident, err := eng.Directory.LookupDID(ctx, did)
	if err != nil {
		eventErrorCount.WithLabelValues("identity").Inc()
		return fmt.Errorf("resolving identity: %w", err)
	}
	if ident == nil {
		eventErrorCount.WithLabelValues("identity").Inc()
		return fmt.Errorf("identity not found for DID: %s", did.String())
	}

	var am *AccountMeta
	if !eng.Config.SkipAccountMeta {
		am, err = eng.GetAccountMeta(ctx, ident)
		if err != nil {
			eventErrorCount.WithLabelValues("identity").Inc()
			return fmt.Errorf("failed to fetch account metadata: %w", err)
		}
	} else {
		am = &AccountMeta{
			Identity: ident,
			Profile:  ProfileSummary{},
		}
	}
	ac := NewAccountContext(ctx, eng, *am)
	if err := eng.Rules.CallIdentityRules(&ac); err != nil {
		eventErrorCount.WithLabelValues("identity").Inc()
		return fmt.Errorf("rule execution failed: %w", err)
	}
	eng.CanonicalLogLineAccount(&ac)
	if eng.Config.PersistSubjectHistoryOzone {
		if err := eng.RerouteIdentityEventToOzone(ctx, &evt); err != nil {
			return fmt.Errorf("failed to persist identity event to ozone history: %w", err)
		}
	}
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

// Entrypoint for external code pushing #account events in to the engine.
//
// This method can be called concurrently, though cached state may end up inconsistent if multiple events for the same account (DID) are processed in parallel.
func (eng *Engine) ProcessAccountEvent(ctx context.Context, evt comatproto.SyncSubscribeRepos_Account) error {
	eventProcessCount.WithLabelValues("account").Inc()
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		eventProcessDuration.WithLabelValues("account").Observe(duration.Seconds())
	}()

	did, err := syntax.ParseDID(evt.Did)
	if err != nil {
		return fmt.Errorf("bad DID in repo #account event (%s): %w", evt.Did, err)
	}

	// similar to an HTTP server, we want to recover any panics from rule execution
	defer func() {
		if r := recover(); r != nil {
			eng.Logger.Error("automod event execution exception", "err", r, "did", did, "type", "account")
			eventErrorCount.WithLabelValues("account").Inc()
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, identityEventTimeout)
	defer cancel()

	// first purge any caches; we need to re-resolve from scratch on account updates
	if err := eng.PurgeAccountCaches(ctx, did); err != nil {
		eng.Logger.Error("failed to purge account cache; account rule may not run correctly", "err", err)
	}
	// TODO(bnewbold): if it was a tombstone, this might fail
	ident, err := eng.Directory.LookupDID(ctx, did)
	if err != nil {
		eventErrorCount.WithLabelValues("account").Inc()
		return fmt.Errorf("resolving identity: %w", err)
	}
	if ident == nil {
		eventErrorCount.WithLabelValues("account").Inc()
		return fmt.Errorf("identity not found for DID: %s", did.String())
	}

	var am *AccountMeta
	if !eng.Config.SkipAccountMeta {
		am, err = eng.GetAccountMeta(ctx, ident)
		if err != nil {
			eventErrorCount.WithLabelValues("identity").Inc()
			return fmt.Errorf("failed to fetch account metadata: %w", err)
		}
	} else {
		am = &AccountMeta{
			Identity: ident,
			Profile:  ProfileSummary{},
		}
	}
	ac := NewAccountContext(ctx, eng, *am)
	if err := eng.Rules.CallAccountRules(&ac); err != nil {
		eventErrorCount.WithLabelValues("account").Inc()
		return fmt.Errorf("rule execution failed: %w", err)
	}
	eng.CanonicalLogLineAccount(&ac)
	if eng.Config.PersistSubjectHistoryOzone {
		if err := eng.RerouteAccountEventToOzone(ctx, &evt); err != nil {
			return fmt.Errorf("failed to persist account event to ozone history: %w", err)
		}
	}
	if err := eng.persistAccountModActions(&ac); err != nil {
		eventErrorCount.WithLabelValues("account").Inc()
		return fmt.Errorf("failed to persist actions for account event: %w", err)
	}
	if err := eng.persistCounters(ctx, ac.effects); err != nil {
		eventErrorCount.WithLabelValues("account").Inc()
		return fmt.Errorf("failed to persist counters for account event: %w", err)
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

	var am *AccountMeta
	if !eng.Config.SkipAccountMeta {
		am, err = eng.GetAccountMeta(ctx, ident)
		if err != nil {
			eventErrorCount.WithLabelValues("identity").Inc()
			return fmt.Errorf("failed to fetch account metadata: %w", err)
		}
	} else {
		am = &AccountMeta{
			Identity: ident,
			Profile:  ProfileSummary{},
		}
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
	if eng.Config.PersistSubjectHistoryOzone {
		if err := eng.RerouteRecordOpToOzone(&rc); err != nil {
			return fmt.Errorf("failed to persist account event to ozone history: %w", err)
		}
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
		"accountTags", c.effects.AccountTags,
		"accountTakedown", c.effects.AccountTakedown,
		"accountReports", len(c.effects.AccountReports),
	)
}

func (e *Engine) CanonicalLogLineRecord(c *RecordContext) {
	c.Logger.Info("canonical-event-line",
		"accountLabels", c.effects.AccountLabels,
		"accountFlags", c.effects.AccountFlags,
		"accountTags", c.effects.AccountTags,
		"accountTakedown", c.effects.AccountTakedown,
		"accountReports", len(c.effects.AccountReports),
		"recordLabels", c.effects.RecordLabels,
		"recordFlags", c.effects.RecordFlags,
		"recordTags", c.effects.RecordTags,
		"recordTakedown", c.effects.RecordTakedown,
		"recordReports", len(c.effects.RecordReports),
	)
}

func (e *Engine) CanonicalLogLineNotification(c *NotificationContext) {
	c.Logger.Info("canonical-event-line",
		"accountLabels", c.effects.AccountLabels,
		"accountFlags", c.effects.AccountFlags,
		"accountTags", c.effects.AccountTags,
		"accountTakedown", c.effects.AccountTakedown,
		"accountReports", len(c.effects.AccountReports),
		"reject", c.effects.RejectEvent,
	)
}
