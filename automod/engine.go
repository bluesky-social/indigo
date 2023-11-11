package automod

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/repomgr"
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

func (e *Engine) ProcessIdentityEvent(t string, did syntax.DID) error {
	ctx := context.Background()

	// similar to an HTTP server, we want to recover any panics from rule execution
	defer func() {
		if r := recover(); r != nil {
			slog.Error("automod event execution exception", "err", r)
			// TODO: mark repo as dirty?
			// TODO: circuit-break on repeated panics?
		}
	}()

	ident, err := e.Directory.LookupDID(ctx, did)
	if err != nil {
		return fmt.Errorf("resolving identity: %w", err)
	}
	if ident == nil {
		return fmt.Errorf("identity not found for did: %s", did.String())
	}

	evt := IdentityEvent{
		Event{
			Engine:  e,
			Account: AccountMeta{Identity: ident},
		},
	}
	e.CallIdentityRules(&evt)

	_ = ctx
	return nil
}

// this method takes a full firehose commit event. it must not be a tooBig
func (e *Engine) ProcessCommit(ctx context.Context, commit *comatproto.SyncSubscribeRepos_Commit) error {

	// similar to an HTTP server, we want to recover any panics from rule execution
	defer func() {
		if r := recover(); r != nil {
			slog.Error("automod event execution exception", "err", r)
			// TODO: mark repo as dirty?
			// TODO: circuit-break on repeated panics?
		}
	}()

	r, err := repo.ReadRepoFromCar(ctx, bytes.NewReader(commit.Blocks))
	if err != nil {
		// TODO: handle this case (instead of return nil)
		slog.Error("reading repo from car", "size_bytes", len(commit.Blocks), "err", err)
		return nil
	}

	did, err := syntax.ParseDID(commit.Repo)
	if err != nil {
		return fmt.Errorf("bad DID syntax in event: %w", err)
	}

	ident, err := e.Directory.LookupDID(ctx, did)
	if err != nil {
		return fmt.Errorf("resolving identity: %w", err)
	}
	if ident == nil {
		return fmt.Errorf("identity not found for did: %s", did.String())
	}

	for _, op := range commit.Ops {
		ek := repomgr.EventKind(op.Action)
		logOp := slog.With("op_path", op.Path, "op_cid", op.Cid)
		switch ek {
		case repomgr.EvtKindCreateRecord:
			rc, rec, err := r.GetRecord(ctx, op.Path)
			if err != nil {
				// TODO: handle this case (instead of return nil)
				logOp.Error("fetching record from event CAR slice", "err", err)
				return nil
			}
			if lexutil.LexLink(rc) != *op.Cid {
				// TODO: handle this case (instead of return nil)
				logOp.Error("mismatch in record and op cid", "record_cid", rc)
				return nil
			}

			if strings.HasPrefix(op.Path, "app.bsky.feed.post/") {
				// TODO: handle as a PostEvent specially
			} else {
				// XXX: pass record in to event
				_ = rec
				evt := RecordEvent{
					Event{
						Engine:  e,
						Account: AccountMeta{Identity: ident},
					},
					[]string{},
					false,
					[]ModReport{},
					[]string{},
				}
				e.CallRecordRules(&evt)
				// TODO persist
			}
		case repomgr.EvtKindUpdateRecord:
			slog.Info("ignoring record update", "did", commit.Repo, "seq", commit.Seq, "path", op.Path)
			return nil
		case repomgr.EvtKindDeleteRecord:
			slog.Info("ignoring record deletion", "did", commit.Repo, "seq", commit.Seq, "path", op.Path)
			return nil
		}
	}

	_ = ctx
	return nil
}

func (e *Engine) CallIdentityRules(evt *IdentityEvent) error {
	return nil
}

func (e *Engine) CallRecordRules(evt *RecordEvent) error {
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
