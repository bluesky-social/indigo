package automod

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/xrpc"
)

// runtime for executing rules, managing state, and recording moderation actions.
//
// TODO: careful when initializing: several fields should not be null or zero, even though they are pointer type.
type Engine struct {
	Logger      *slog.Logger
	Directory   identity.Directory
	Rules       RuleSet
	Counters    CountStore
	Sets        SetStore
	Cache       CacheStore
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
	if err := evt.PersistActions(ctx); err != nil {
		return err
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
	if err := evt.PersistActions(ctx); err != nil {
		return err
	}
	if err := evt.PersistCounters(ctx); err != nil {
		return err
	}
	// TODO: refactor this in to PersistActions? after mod api v2 merge
	if e.SlackWebhookURL != "" && (len(evt.AccountLabels) > 0 || len(evt.RecordLabels) > 0) {
		msg := fmt.Sprintf("⚠️ Automod Action ⚠️\n")
		msg += fmt.Sprintf("DID: %s\n", am.Identity.DID)
		msg += fmt.Sprintf("Account Labels: %s\n", evt.AccountLabels)
		msg += fmt.Sprintf("Record Labels: %s\n", evt.RecordLabels)
		if err := e.SendSlackMsg(ctx, msg); err != nil {
			return err
		}
	}

	return nil
}

func (e *Engine) FetchAndProcessRecord(ctx context.Context, uri string) error {
	// resolve URI, identity, and record
	aturi, err := syntax.ParseATURI(uri)
	if err != nil {
		return fmt.Errorf("parsing AT-URI argument: %v", err)
	}
	if aturi.RecordKey() == "" {
		return fmt.Errorf("need a full, not partial, AT-URI: %s", uri)
	}
	ident, err := e.Directory.Lookup(ctx, aturi.Authority())
	if err != nil {
		return fmt.Errorf("resolving AT-URI authority: %v", err)
	}
	pdsURL := ident.PDSEndpoint()
	if pdsURL == "" {
		return fmt.Errorf("could not resolve PDS endpoint for AT-URI account: %s", ident.DID.String())
	}
	pdsClient := xrpc.Client{Host: ident.PDSEndpoint()}

	e.Logger.Info("fetching record", "did", ident.DID.String(), "collection", aturi.Collection().String(), "rkey", aturi.RecordKey().String())
	out, err := comatproto.RepoGetRecord(ctx, &pdsClient, "", aturi.Collection().String(), ident.DID.String(), aturi.RecordKey().String())
	if err != nil {
		return fmt.Errorf("fetching record from Relay (%s): %v", aturi, err)
	}
	if out.Cid == nil {
		return fmt.Errorf("expected a CID in getRecord response")
	}
	return e.ProcessRecord(ctx, ident.DID, aturi.Path(), *out.Cid, out.Value.Val)
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

func (e *Engine) GetCount(name, val, period string) (int, error) {
	return e.Counters.GetCount(context.TODO(), name, val, period)
}

// checks if `val` is an element of set `name`
func (e *Engine) InSet(name, val string) (bool, error) {
	return e.Sets.InSet(context.TODO(), name, val)
}
