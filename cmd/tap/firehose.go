package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/repo"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/cmd/tap/models"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/events/schedulers/parallel"
	"github.com/gorilla/websocket"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

var tracer = otel.Tracer("tap")

type FirehoseProcessor struct {
	logger *slog.Logger
	db     *gorm.DB

	events *EventManager
	repos  *RepoManager

	relayUrl           string
	fullNetworkMode    bool
	signalCollection   string
	collectionFilters  []string
	parallelism        int
	cursorSaveInterval time.Duration

	lastSeq atomic.Int64
}

func NewFirehoseProcessor(logger *slog.Logger, db *gorm.DB, events *EventManager, repos *RepoManager, config *TapConfig) *FirehoseProcessor {
	return &FirehoseProcessor{
		logger:             logger.With("component", "firehose"),
		db:                 db,
		events:             events,
		repos:              repos,
		relayUrl:           config.RelayUrl,
		fullNetworkMode:    config.FullNetworkMode,
		signalCollection:   config.SignalCollection,
		collectionFilters:  config.CollectionFilters,
		parallelism:        config.FirehoseParallelism,
		cursorSaveInterval: config.FirehoseCursorSaveInterval,
	}
}

func (fp *FirehoseProcessor) updateLastSeq(seq int64) {
	fp.lastSeq.Store(seq)
	firehoseLastSeq.Set(float64(seq))
}

func (fp *FirehoseProcessor) Run(ctx context.Context) error {
	fp.events.WaitForReady(ctx)

	go fp.RunCursorSaver(ctx)
	return fp.runConsumer(ctx)
}

// ProcessCommit validates and applies a commit event from the firehose.
func (fp *FirehoseProcessor) ProcessCommit(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Commit) error {
	firehoseEventsReceived.Inc()

	ctx, span := tracer.Start(ctx, "ProcessCommit")
	defer span.End()

	defer fp.updateLastSeq(evt.Seq)

	curr, err := fp.repos.GetRepoState(ctx, evt.Repo)
	if err != nil {
		return err
	} else if curr == nil {
		shouldTrack := fp.fullNetworkMode || (fp.signalCollection != "" && evtHasSignalCollection(evt, fp.signalCollection))
		if shouldTrack {
			if err := fp.repos.EnsureRepo(ctx, evt.Repo); err != nil {
				fp.logger.Error("failed to auto-track repo", "did", evt.Repo, "error", err)
				return err
			}
		}
		// even if we just tracked, we return here and just let the resync workers handle the rest
		firehoseEventsSkipped.Inc()
		return nil
	}

	if curr.State != models.RepoStateActive && curr.State != models.RepoStateResyncing {
		firehoseEventsSkipped.Inc()
		return nil
	}

	if curr.Rev != "" && evt.Rev <= curr.Rev {
		fp.logger.Debug("skipping replayed event", "did", evt.Repo, "eventRev", evt.Rev, "currentRev", curr.Rev)
		firehoseEventsSkipped.Inc()
		return nil
	}

	commit, err := fp.validateCommitAndFilterOps(ctx, evt)
	if err != nil {
		fp.logger.Error("failed to parse operations", "did", evt.Repo, "error", err)
		return err
	}

	if curr.State == models.RepoStateResyncing {
		firehoseEventsSkipped.Inc()
		return fp.events.addToResyncBuffer(ctx, commit)
	}

	if evt.PrevData == nil {
		fp.logger.Debug("legacy commit event, skipping prev data check", "did", evt.Repo, "rev", evt.Rev)
	} else if evt.PrevData.String() != curr.PrevData {
		fp.logger.Warn("repo state desynchronized", "did", evt.Repo, "rev", evt.Rev)
		// gets picked up by resync workers
		if err := fp.repos.UpdateRepoState(ctx, evt.Repo, models.RepoStateDesynchronized); err != nil {
			fp.logger.Error("failed to update repo state to desynchronized", "did", evt.Repo, "error", err)
			return err
		}
		return nil
	}

	if err := fp.events.AddCommit(ctx, commit, func(tx *gorm.DB) error {
		return nil
	}); err != nil {
		fp.logger.Error("failed to update repo state", "did", commit.Did, "rev", commit.Rev, "error", err)
		return err
	}

	firehoseEventsProcessed.Inc()
	return nil
}

func (fp *FirehoseProcessor) validateCommitAndFilterOps(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Commit) (*Commit, error) {
	if err := repo.VerifyCommitSignature(ctx, fp.repos.idDir, evt); err != nil {
		return nil, err
	}

	r, err := repo.VerifyCommitMessage(ctx, evt)
	if err != nil {
		return nil, err
	}

	parsedOps := make([]CommitOp, 0)

	for _, op := range evt.Ops {
		collection, rkey, err := syntax.ParseRepoPath(op.Path)
		if err != nil {
			return nil, fmt.Errorf("invalid record path: %w", err)
		}

		parsed := CommitOp{
			Collection: collection.String(),
			Rkey:       rkey.String(),
			Action:     op.Action,
		}

		if op.Action == "create" || op.Action == "update" {
			if op.Cid == nil {
				return nil, fmt.Errorf("missing CID for create/update: %s", op.Path)
			}
			parsed.Cid = op.Cid.String()

			recBytes, _, err := r.GetRecordBytes(ctx, collection, rkey)
			if err != nil {
				fp.logger.Error("failed to get record bytes", "did", evt.Repo, "path", op.Path, "error", err)
				continue
			}

			record, err := atdata.UnmarshalCBOR(recBytes)
			if err != nil {
				// do not skip here
				// we end up storing the CID but not passing the record along in the outbox
				fp.logger.Error("failed to unmarshal record", "did", evt.Repo, "path", op.Path, "error", err)
			}
			parsed.Record = record
		}

		if matchesCollection(parsed.Collection, fp.collectionFilters) {
			parsedOps = append(parsedOps, parsed)
		}
	}

	dataCid, err := r.MST.RootCID()
	if err != nil {
		return nil, err
	}

	commit := &Commit{
		Did:     evt.Repo,
		Rev:     evt.Rev,
		DataCid: dataCid.String(),
		Ops:     parsedOps,
	}

	if evt.PrevData != nil {
		commit.PrevData = evt.PrevData.String()
	}

	return commit, nil
}

// ProcessSync handles sync events and marks repos for resync if needed.
func (fp *FirehoseProcessor) ProcessSync(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Sync) error {
	firehoseEventsReceived.Inc()

	ctx, span := tracer.Start(ctx, "ProcessSync")
	defer span.End()

	defer fp.updateLastSeq(evt.Seq)

	curr, err := fp.repos.GetRepoState(ctx, evt.Did)
	if err != nil {
		return err
	} else if curr == nil {
		if fp.fullNetworkMode {
			if err := fp.repos.EnsureRepo(ctx, evt.Did); err != nil {
				fp.logger.Error("failed to auto-track repo", "did", evt.Did, "error", err)
				return err
			}
			firehoseEventsSkipped.Inc()
			return nil
		}
		firehoseEventsSkipped.Inc()
		return nil
	}

	commit, err := repo.VerifySyncMessage(ctx, fp.repos.idDir, evt)
	if err != nil {
		return fmt.Errorf("failed to verify sync message: %w", err)
	}

	if curr.State != models.RepoStateActive {
		firehoseEventsSkipped.Inc()
		return nil
	}

	if curr.Rev != "" && commit.Rev <= curr.Rev {
		fp.logger.Debug("skipping replayed event", "did", commit.DID, "eventRev", commit.Rev, "currentRev", curr.Rev)
		firehoseEventsSkipped.Inc()
		return nil
	}

	if curr.PrevData == commit.Data.String() {
		fp.logger.Debug("skipping noop sync event", "did", commit.DID, "rev", commit.Rev)
		firehoseEventsSkipped.Inc()
		return nil
	}

	if err := fp.repos.UpdateRepoState(ctx, commit.DID, models.RepoStateDesynchronized); err != nil {
		fp.logger.Error("failed to update repo state to desynchronized", "did", commit.DID, "error", err)
		return err
	}

	firehoseEventsProcessed.Inc()
	return nil
}

func (fp *FirehoseProcessor) ProcessIdentity(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Identity) error {
	firehoseEventsReceived.Inc()
	defer fp.updateLastSeq(evt.Seq)

	curr, err := fp.repos.GetRepoState(ctx, evt.Did)
	if err != nil {
		return err
	} else if curr == nil {
		if fp.fullNetworkMode {
			if err := fp.repos.EnsureRepo(ctx, evt.Did); err != nil {
				return err
			}
			firehoseEventsSkipped.Inc()
			return nil
		}
		firehoseEventsSkipped.Inc()
		return nil
	}

	if err := fp.repos.RefreshIdentity(ctx, evt.Did); err != nil {
		return err
	}

	firehoseEventsProcessed.Inc()
	return nil
}

func (fp *FirehoseProcessor) ProcessAccount(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Account) error {
	firehoseEventsReceived.Inc()
	defer fp.updateLastSeq(evt.Seq)

	curr, err := fp.repos.GetRepoState(ctx, evt.Did)
	if err != nil {
		return err
	} else if curr == nil {
		if fp.fullNetworkMode && evt.Active {
			if err := fp.repos.EnsureRepo(ctx, evt.Did); err != nil {
				fp.logger.Error("failed to auto-track repo", "did", evt.Did, "error", err)
				return err
			}
			firehoseEventsSkipped.Inc()
			return nil
		}
		firehoseEventsSkipped.Inc()
		return nil
	}

	var updateTo models.AccountStatus
	if evt.Active {
		updateTo = models.AccountStatusActive
	} else if evt.Status != nil && (*evt.Status == string(models.AccountStatusDeactivated) || *evt.Status == string(models.AccountStatusTakendown) || *evt.Status == string(models.AccountStatusSuspended) || *evt.Status == string(models.AccountStatusDeleted)) {
		updateTo = models.AccountStatus(*evt.Status)
	} else {
		// no-op for other events such as throttled or desynchronized
		firehoseEventsSkipped.Inc()
		return nil
	}

	if curr.Status == updateTo {
		firehoseEventsSkipped.Inc()
		return nil
	}

	identityEvt := &IdentityEvt{
		Did:      curr.Did,
		Handle:   curr.Handle,
		IsActive: evt.Active,
		Status:   updateTo,
	}

	if updateTo == models.AccountStatusDeleted {
		if err := fp.events.AddIdentityEvent(ctx, identityEvt, func(tx *gorm.DB) error {
			return deleteRepo(tx, evt.Did)
		}); err != nil {
			fp.logger.Error("failed to delete repo", "did", evt.Did, "error", err)
			return err
		}
	} else {
		if err := fp.events.AddIdentityEvent(ctx, identityEvt, func(tx *gorm.DB) error {
			return tx.Model(&models.Repo{}).
				Where("did = ?", evt.Did).
				Update("status", updateTo).Error
		}); err != nil {
			fp.logger.Error("failed to update repo status", "did", evt.Did, "status", updateTo, "error", err)
			return err
		}
	}

	firehoseEventsProcessed.Inc()
	return nil
}

func (fp *FirehoseProcessor) saveCursor(ctx context.Context) error {
	seq := fp.lastSeq.Load()
	if seq < 1 {
		return nil
	}

	return fp.db.WithContext(ctx).Save(&models.FirehoseCursor{
		Url:    fp.relayUrl,
		Cursor: seq,
	}).Error
}

// RunCursorSaver periodically saves the firehose cursor to the database.
func (fp *FirehoseProcessor) RunCursorSaver(ctx context.Context) {
	runPeriodically(ctx, fp.cursorSaveInterval, func(ctx context.Context) error {
		if err := fp.saveCursor(ctx); err != nil {
			fp.logger.Error("failed to save cursor", "error", err, "relayUrl", fp.relayUrl)
		}
		return nil // don't exit, just log error
	})

	// save cursor one last time on shutdown
	if err := fp.saveCursor(ctx); err != nil {
		fp.logger.Error("failed to save cursor on shutdown", "error", err, "relayUrl", fp.relayUrl)
	}
}

func (fp *FirehoseProcessor) GetCursor(ctx context.Context) (int64, error) {
	var cursor models.FirehoseCursor
	if err := fp.db.WithContext(ctx).Where("url = ?", fp.relayUrl).First(&cursor).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			fp.logger.Info("no pre-existing cursor in database", "relayUrl", fp.relayUrl)
			return 0, nil
		}
		return 0, err
	}
	return cursor.Cursor, nil
}

// Connects to the firehose and processes events until context cancellation
// On error, restarts the connection
func (fp *FirehoseProcessor) runConsumer(ctx context.Context) error {
	u, err := url.Parse(fp.relayUrl)
	if err != nil {
		return fmt.Errorf("invalid relay URL: %w", err)
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	}
	u.Path = "xrpc/com.atproto.sync.subscribeRepos"

	rsc := &events.RepoStreamCallbacks{
		RepoCommit: func(evt *comatproto.SyncSubscribeRepos_Commit) error {
			return fp.ProcessCommit(ctx, evt)
		},
		RepoSync: func(evt *comatproto.SyncSubscribeRepos_Sync) error {
			return fp.ProcessSync(ctx, evt)
		},
		RepoIdentity: func(evt *comatproto.SyncSubscribeRepos_Identity) error {
			return fp.ProcessIdentity(ctx, evt)
		},
		RepoAccount: func(evt *comatproto.SyncSubscribeRepos_Account) error {
			return fp.ProcessAccount(ctx, evt)
		},
	}

	var retries int
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		cursor, err := fp.GetCursor(ctx)
		if err != nil {
			return fmt.Errorf("failed to read cursor: %w", err)
		}

		if cursor > 0 {
			u.RawQuery = fmt.Sprintf("cursor=%d", cursor)
		}
		urlStr := u.String()

		fp.logger.Info("connecting to firehose", "url", urlStr, "cursor", cursor, "retries", retries)

		dialer := websocket.DefaultDialer
		con, _, err := dialer.DialContext(ctx, urlStr, http.Header{
			"User-Agent": []string{userAgent()},
		})
		if err != nil {
			fp.logger.Warn("dialing failed", "error", err, "retries", retries)
			time.Sleep(backoff(retries, 10))
			retries++
			continue
		}

		fp.logger.Info("connected to firehose")
		retries = 0

		scheduler := parallel.NewScheduler(
			fp.parallelism,
			100,
			fp.relayUrl,
			rsc.EventHandler,
		)
		if err := events.HandleRepoStream(ctx, con, scheduler, nil); err != nil {
			fp.logger.Warn("firehose connection failed", "error", err)
		}
	}
}
