package automod

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
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
	AdminClient *xrpc.Client
}

func (e *Engine) ProcessIdentityEvent(ctx context.Context, t string, did syntax.DID) error {
	// similar to an HTTP server, we want to recover any panics from rule execution
	defer func() {
		if r := recover(); r != nil {
			e.Logger.Error("automod event execution exception", "err", r)
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
		Event{
			Engine:  e,
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
			e.Logger.Error("automod event execution exception", "err", r)
		}
	}()

	ident, err := e.Directory.LookupDID(ctx, did)
	if err != nil {
		return fmt.Errorf("resolving identity: %w", err)
	}
	if ident == nil {
		return fmt.Errorf("identity not found for did: %s", did.String())
	}
	collection := strings.SplitN(path, "/", 2)[0]

	switch collection {
	case "app.bsky.feed.post":
		post, ok := rec.(*appbsky.FeedPost)
		if !ok {
			return fmt.Errorf("mismatch between collection (%s) and type", collection)
		}
		am, err := e.GetAccountMeta(ctx, ident)
		if err != nil {
			return err
		}
		evt := e.NewPostEvent(*am, path, recCID, post)
		e.Logger.Debug("processing post", "did", ident.DID, "path", path)
		if err := e.Rules.CallPostRules(&evt); err != nil {
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
	default:
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
	if e.RelayClient == nil {
		return fmt.Errorf("can't fetch record without relay client configured")
	}
	ident, err := e.Directory.Lookup(ctx, aturi.Authority())
	if err != nil {
		return fmt.Errorf("resolving AT-URI authority: %v", err)
	}
	e.Logger.Info("fetching record", "did", ident.DID.String(), "collection", aturi.Collection().String(), "rkey", aturi.RecordKey().String())
	out, err := comatproto.RepoGetRecord(ctx, e.RelayClient, "", aturi.Collection().String(), ident.DID.String(), aturi.RecordKey().String())
	if err != nil {
		return fmt.Errorf("fetching record from Relay (%s): %v", aturi, err)
	}
	if out.Cid == nil {
		return fmt.Errorf("expected a CID in getRecord response")
	}
	return e.ProcessRecord(ctx, ident.DID, aturi.Path(), *out.Cid, out.Value.Val)
}

func (e *Engine) NewPostEvent(am AccountMeta, path, recCID string, post *appbsky.FeedPost) PostEvent {
	parts := strings.SplitN(path, "/", 2)
	return PostEvent{
		RecordEvent{
			Event{
				Engine:  e,
				Logger:  e.Logger.With("did", am.Identity.DID, "collection", parts[0], "rkey", parts[1]),
				Account: am,
			},
			parts[0],
			parts[1],
			recCID,
			[]string{},
			false,
			[]ModReport{},
			[]string{},
		},
		post,
	}
}

func (e *Engine) NewRecordEvent(am AccountMeta, path, recCID string, rec any) RecordEvent {
	parts := strings.SplitN(path, "/", 2)
	return RecordEvent{
		Event{
			Engine:  e,
			Logger:  e.Logger.With("did", am.Identity.DID, "collection", parts[0], "rkey", parts[1]),
			Account: am,
		},
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
