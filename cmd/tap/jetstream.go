package main

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	"slices"

	"github.com/bluesky-social/indigo/cmd/tap/jetstream"
	"github.com/bluesky-social/indigo/cmd/tap/models"
	"github.com/gorilla/websocket"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

var jetstreamTracer = otel.Tracer("tap")

type JetstreamProcessor struct {
	logger *slog.Logger
	db     *gorm.DB

	events *EventManager
	repos  *RepoManager

	jetstreamUrl               string
	fullNetworkMode            bool
	signalCollection           string
	lightRailSignalCollections []string
	collectionFilters          []string
	parallelism                int
	cursorSaveInterval         time.Duration
	noReplay                   bool

	lastTimeUs atomic.Int64
}

func NewJetstreamProcessor(logger *slog.Logger, db *gorm.DB, events *EventManager, repos *RepoManager, config *TapConfig) *JetstreamProcessor {
	return &JetstreamProcessor{
		logger:             logger.With("component", "jetstream"),
		db:                 db,
		events:             events,
		repos:              repos,
		jetstreamUrl:       config.JetstreamUrl,
		fullNetworkMode:    config.FullNetworkMode,
		signalCollection:   config.SignalCollection,
		collectionFilters:  config.CollectionFilters,
		parallelism:        config.FirehoseParallelism,
		cursorSaveInterval: config.FirehoseCursorSaveInterval,
		noReplay:           config.NoReplay,
	}
}

func (jp *JetstreamProcessor) updateLastSeq(seq int64) {
	jp.lastTimeUs.Store(seq)
	jetstreamLastSeq.Set(float64(seq))
}

func (fp *JetstreamProcessor) Run(ctx context.Context) error {
	fp.events.WaitForReady(ctx)

	go fp.RunCursorSaver(ctx)
	return fp.runConsumer(ctx)
}

// ProcessCommit  applies a commit event from the Jetstream.
func (jp *JetstreamProcessor) ProcessCommit(ctx context.Context, evt *jetstream.JetstreamEvent) error {
	firehoseEventsReceived.Inc()

	ctx, span := tracer.Start(ctx, "ProcessCommit")
	defer span.End()

	defer jp.updateLastSeq(evt.TimeUS)

	curr, err := jp.repos.GetRepoState(ctx, evt.Did)
	if err != nil {
		return err
	} else if curr == nil {
		collection := evt.Commit.Collection
		shouldTrack := jp.fullNetworkMode || jp.signalCollection != "" && jp.signalCollection == collection || slices.Contains(jp.lightRailSignalCollections, collection)
		if shouldTrack {
			if err := jp.repos.EnsureRepo(ctx, evt.Did); err != nil {
				jp.logger.Error("failed to auto-track repo", "did", evt.Did, "error", err)
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

	if curr.Rev != "" && evt.Commit.Rev <= curr.Rev {
		jp.logger.Debug("skipping replayed event", "did", evt.Did, "eventRev", evt.Commit.Rev, "currentRev", curr.Rev)
		firehoseEventsSkipped.Inc()
		return nil
	}

	if !matchesCollection(evt.Commit.Collection, jp.collectionFilters) {
		firehoseEventsSkipped.Inc()
		return nil
	}

	commit := &Commit{
		Did: evt.Did,
		Rev: evt.Commit.Rev,
		Ops: []CommitOp{
			{
				Collection: evt.Commit.Collection,
				Rkey:       evt.Commit.RKey,
				Action:     evt.Commit.Operation,
				//TODO may not like that type change will have to see
				Record: evt.Commit.Record,
				Cid:    evt.Commit.CID,
			},
		},
	}
	if curr.State == models.RepoStateResyncing {
		firehoseEventsSkipped.Inc()
		return jp.events.addToResyncBuffer(ctx, commit)
	}

	// Jetstream does not have prevdata but still may come back
	// if evt.PrevData == nil {
	// 	fp.logger.Debug("legacy commit event, skipping prev data check", "did", evt.Repo, "rev", evt.Rev)
	// } else if evt.PrevData.String() != curr.PrevData {
	// 	fp.logger.Warn("repo state desynchronized", "did", evt.Repo, "rev", evt.Rev)
	// 	// gets picked up by resync workers
	// 	if err := fp.repos.UpdateRepoState(ctx, evt.Repo, models.RepoStateDesynchronized); err != nil {
	// 		fp.logger.Error("failed to update repo state to desynchronized", "did", evt.Repo, "error", err)
	// 		return err
	// 	}
	// 	return nil
	// }

	if err := jp.events.AddCommit(ctx, commit, func(tx *gorm.DB) error {
		return nil
	}); err != nil {
		jp.logger.Error("failed to update repo state", "did", commit.Did, "rev", commit.Rev, "error", err)
		return err
	}

	firehoseEventsProcessed.Inc()
	return nil
}

// ProcessSync handles sync events and marks repos for resync if needed.
func (jp *JetstreamProcessor) ProcessSync(ctx context.Context, evt *jetstream.JetstreamEvent) error {
	firehoseEventsReceived.Inc()

	ctx, span := tracer.Start(ctx, "ProcessSync")
	defer span.End()

	defer jp.updateLastSeq(evt.TimeUS)

	curr, err := jp.repos.GetRepoState(ctx, evt.Did)
	if err != nil {
		return err
	} else if curr == nil {
		if jp.fullNetworkMode {
			if err := jp.repos.EnsureRepo(ctx, evt.Did); err != nil {
				jp.logger.Error("failed to auto-track repo", "did", evt.Did, "error", err)
				return err
			}
			firehoseEventsSkipped.Inc()
			return nil
		}
		firehoseEventsSkipped.Inc()
		return nil
	}

	if curr.State != models.RepoStateActive {
		firehoseEventsSkipped.Inc()
		return nil
	}

	if curr.Rev != "" && evt.Commit.RKey <= curr.Rev {
		jp.logger.Debug("skipping replayed event", "did", evt.Did, "eventRev", evt.Commit.Rev, "currentRev", curr.Rev)
		firehoseEventsSkipped.Inc()
		return nil
	}

	// We just resync on all sync events since we don't have access to the commit data
	if err := jp.repos.UpdateRepoState(ctx, evt.Did, models.RepoStateDesynchronized); err != nil {
		jp.logger.Error("failed to update repo state to desynchronized", "did", evt.Did, "error", err)
		return err
	}

	firehoseEventsProcessed.Inc()
	return nil
}

func (jp *JetstreamProcessor) ProcessIdentity(ctx context.Context, evt *jetstream.JetstreamEvent) error {
	firehoseEventsReceived.Inc()
	defer jp.updateLastSeq(evt.TimeUS)

	curr, err := jp.repos.GetRepoState(ctx, evt.Did)
	if err != nil {
		return err
	} else if curr == nil {
		if jp.fullNetworkMode {
			if err := jp.repos.EnsureRepo(ctx, evt.Did); err != nil {
				return err
			}
			firehoseEventsSkipped.Inc()
			return nil
		}
		firehoseEventsSkipped.Inc()
		return nil
	}

	if err := jp.repos.RefreshIdentity(ctx, evt.Did); err != nil {
		return err
	}

	firehoseEventsProcessed.Inc()
	return nil
}

func (jp *JetstreamProcessor) ProcessAccount(ctx context.Context, evt *jetstream.JetstreamEvent) error {
	firehoseEventsReceived.Inc()
	defer jp.updateLastSeq(evt.TimeUS)

	curr, err := jp.repos.GetRepoState(ctx, evt.Did)
	if err != nil {
		return err
	} else if curr == nil {
		if jp.fullNetworkMode && evt.Account.Active {
			if err := jp.repos.EnsureRepo(ctx, evt.Did); err != nil {
				jp.logger.Error("failed to auto-track repo", "did", evt.Did, "error", err)
				return err
			}
			firehoseEventsSkipped.Inc()
			return nil
		}
		firehoseEventsSkipped.Inc()
		return nil
	}

	var updateTo models.AccountStatus
	if evt.Account.Active {
		updateTo = models.AccountStatusActive
	} else if evt.Account.Status != nil && (*evt.Account.Status == string(models.AccountStatusDeactivated) || *evt.Account.Status == string(models.AccountStatusTakendown) || *evt.Account.Status == string(models.AccountStatusSuspended) || *evt.Account.Status == string(models.AccountStatusDeleted)) {
		updateTo = models.AccountStatus(*evt.Account.Status)
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
		IsActive: evt.Account.Active,
		Status:   updateTo,
	}

	if updateTo == models.AccountStatusDeleted {
		if err := jp.events.AddIdentityEvent(ctx, identityEvt, func(tx *gorm.DB) error {
			return deleteRepo(tx, evt.Did)
		}); err != nil {
			jp.logger.Error("failed to delete repo", "did", evt.Did, "error", err)
			return err
		}
	} else {
		if err := jp.events.AddIdentityEvent(ctx, identityEvt, func(tx *gorm.DB) error {
			return tx.Model(&models.Repo{}).
				Where("did = ?", evt.Did).
				Update("status", updateTo).Error
		}); err != nil {
			jp.logger.Error("failed to update repo status", "did", evt.Did, "status", updateTo, "error", err)
			return err
		}
	}

	firehoseEventsProcessed.Inc()
	return nil
}

func (jp *JetstreamProcessor) saveCursor(ctx context.Context) error {
	lastTimeUs := jp.lastTimeUs.Load()
	if lastTimeUs < 1 {
		return nil
	}

	return jp.db.WithContext(ctx).Save(&models.FirehoseCursor{
		Url:    jp.jetstreamUrl,
		Cursor: lastTimeUs,
	}).Error
}

// RunCursorSaver periodically saves the firehose cursor to the database.
func (jp *JetstreamProcessor) RunCursorSaver(ctx context.Context) {
	runPeriodically(ctx, jp.cursorSaveInterval, func(ctx context.Context) error {
		if err := jp.saveCursor(ctx); err != nil {
			jp.logger.Error("failed to save cursor", "error", err, "jetstreamUrl", jp.jetstreamUrl)
		}
		return nil // don't exit, just log error
	})

	// save cursor one last time on shutdown
	if err := jp.saveCursor(ctx); err != nil {
		jp.logger.Error("failed to save cursor on shutdown", "error", err, "jetstreamUrl", jp.jetstreamUrl)
	}
}

func (jp *JetstreamProcessor) GetCursor(ctx context.Context) (int64, error) {
	if jp.noReplay {
		jp.logger.Info("jetstream replay disabled, skipping to live")
		return 0, nil
	}

	var cursor models.FirehoseCursor
	if err := jp.db.WithContext(ctx).Where("url = ?", jp.jetstreamUrl).First(&cursor).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			jp.logger.Info("no pre-existing cursor in database", "jetstreamUrl", jp.jetstreamUrl)
			return 0, nil
		}
		return 0, err
	}
	return cursor.Cursor, nil
}

// Connects to the jetstream and processes events until context cancellation
// On error, restarts the connection
func (jp *JetstreamProcessor) runConsumer(ctx context.Context) error {
	u, err := url.Parse(jp.jetstreamUrl)
	if err != nil {
		return fmt.Errorf("invalid jetstream URL: %w", err)
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	}
	u.Path = "/subscribe"
	query := u.Query()
	query.Add("compress", "true")

	//Probably add a note in the readme not to use jetstream if you want full network mode. May save some bandwidth
	// but in for a penny in for a pound
	if !jp.fullNetworkMode {
		if jp.signalCollection != "" {
			query.Set("wantedCollection", jp.signalCollection)
		} else if len(jp.lightRailSignalCollections) > 0 {
			for _, collection := range jp.lightRailSignalCollections {
				query.Set("wantedCollection", collection)
			}
		} else if len(jp.collectionFilters) > 0 {
			for _, filter := range jp.collectionFilters {
				query.Set("wantedCollection", filter)
			}
		}
	}
	u.RawQuery = query.Encode()

	rsc := &jetstream.RepoJetStreamCallbacks{
		Commit: func(evt *jetstream.JetstreamEvent) error {
			return jp.ProcessCommit(ctx, evt)
		},
		Identity: func(evt *jetstream.JetstreamEvent) error {
			return jp.ProcessIdentity(ctx, evt)
		},
		Account: func(evt *jetstream.JetstreamEvent) error {
			return jp.ProcessAccount(ctx, evt)
		},
	}

	var retries int
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		cursor, err := jp.GetCursor(ctx)
		if err != nil {
			return fmt.Errorf("failed to read cursor: %w", err)
		}

		if cursor > 0 {
			u.RawQuery = fmt.Sprintf("cursor=%d", cursor)
		}
		urlStr := u.String()

		jp.logger.Info("connecting to jetstream", "url", urlStr, "cursor", cursor, "retries", retries)

		dialer := websocket.DefaultDialer
		con, _, err := dialer.DialContext(ctx, urlStr, http.Header{
			"User-Agent": []string{userAgent()},
		})
		if err != nil {
			jp.logger.Warn("dialing failed", "error", err, "retries", retries)
			time.Sleep(backoff(retries, 10))
			retries++
			continue
		}

		jp.logger.Info("connected to firehose")
		retries = 0

		// TODO
		// Move all of this to a jetstream folder probably
		// Make a new type like XRPCStreamEvent, but for the jetstream
		// write a NewScheduler that takes the new type
		// Finish writing the new consumer
		// finish the top actions
		// profit $$$

		scheduler := jetstream.NewScheduler(
			jp.parallelism,
			100,
			jp.jetstreamUrl,
			rsc.EventHandler,
		)
		if err := jetstream.HandleJetStream(ctx, con, scheduler, nil); err != nil {
			jp.logger.Warn("jetstream connection failed", "error", err)
		}
	}
}
