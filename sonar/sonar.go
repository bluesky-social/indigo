package sonar

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/araddon/dateparse"
	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/api/bsky"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/goccy/go-json"

	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/repomgr"
	"go.opentelemetry.io/otel"
)

type Sonar struct {
	SocketURL  string
	Progress   *Progress
	ProgMux    sync.Mutex
	Logger     *slog.Logger
	CursorFile string
}

type Progress struct {
	LastSeq            int64     `json:"last_seq"`
	LastSeqProcessedAt time.Time `json:"last_seq_processed_at"`
}

func (s *Sonar) WriteCursorFile() error {
	// Marshal the cursor file
	s.ProgMux.Lock()
	data, err := json.Marshal(s.Progress)
	s.ProgMux.Unlock()
	if err != nil {
		return fmt.Errorf("failed to marshal cursor file: %+v", err)
	}

	// Write the cursor file
	err = os.WriteFile(s.CursorFile, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write cursor file: %+v", err)
	}

	return nil
}

func (s *Sonar) ReadCursorFile() error {
	// Read the cursor file
	data, err := os.ReadFile(s.CursorFile)
	if err != nil {
		return fmt.Errorf("failed to read cursor file: %+v", err)
	}

	// Unmarshal the cursor file
	s.ProgMux.Lock()
	err = json.Unmarshal(data, s.Progress)
	s.ProgMux.Unlock()
	if err != nil {
		return fmt.Errorf("failed to unmarshal cursor file: %+v", err)
	}

	return nil
}

func NewSonar(logger *slog.Logger, cursorFile string, socketURL string) (*Sonar, error) {
	s := Sonar{
		SocketURL: socketURL,
		Progress: &Progress{
			LastSeq: -1,
		},
		Logger:     logger,
		ProgMux:    sync.Mutex{},
		CursorFile: cursorFile,
	}

	// Check to see if the cursor file exists
	if _, err := os.Stat(cursorFile); os.IsNotExist(err) {
		logger.Info("cursor file does not exist, creating", "path", cursorFile)
		// Create the cursor file
		err := s.WriteCursorFile()
		if err != nil {
			return nil, fmt.Errorf("failed to write cursor file: %+v", err)
		}
	} else {
		// Read the cursor file
		err := s.ReadCursorFile()
		if err != nil {
			logger.Error("read cursor file, will start drinking from live", "err", err.Error())
		}
	}

	return &s, nil
}

func (s *Sonar) HandleStreamEvent(ctx context.Context, xe *events.XRPCStreamEvent) error {
	ctx, span := otel.Tracer("sonar").Start(ctx, "HandleStreamEvent")
	defer span.End()

	switch {
	case xe.RepoCommit != nil:
		eventsProcessedCounter.WithLabelValues("repo_commit", s.SocketURL).Inc()
		return s.HandleRepoCommit(ctx, xe.RepoCommit)
	case xe.RepoHandle != nil:
		eventsProcessedCounter.WithLabelValues("repo_handle", s.SocketURL).Inc()
		now := time.Now()
		s.ProgMux.Lock()
		s.Progress.LastSeq = xe.RepoHandle.Seq
		s.Progress.LastSeqProcessedAt = now
		s.ProgMux.Unlock()
		// Parse time from the event time string
		t, err := time.Parse(time.RFC3339, xe.RepoHandle.Time)
		if err != nil {
			s.Logger.Error("error parsing time", "err", err)
			return nil
		}
		lastEvtCreatedAtGauge.WithLabelValues(s.SocketURL).Set(float64(t.UnixNano()))
		lastEvtProcessedAtGauge.WithLabelValues(s.SocketURL).Set(float64(now.UnixNano()))
		lastEvtCreatedEvtProcessedGapGauge.WithLabelValues(s.SocketURL).Set(float64(now.Sub(t).Seconds()))
		lastSeqGauge.WithLabelValues(s.SocketURL).Set(float64(xe.RepoHandle.Seq))
	case xe.RepoIdentity != nil:
		eventsProcessedCounter.WithLabelValues("identity", s.SocketURL).Inc()
		now := time.Now()
		s.ProgMux.Lock()
		s.Progress.LastSeq = xe.RepoIdentity.Seq
		s.Progress.LastSeqProcessedAt = now
		s.ProgMux.Unlock()
	case xe.RepoAccount != nil:
		eventsProcessedCounter.WithLabelValues("account", s.SocketURL).Inc()
		now := time.Now()
		s.ProgMux.Lock()
		s.Progress.LastSeq = xe.RepoAccount.Seq
		s.Progress.LastSeqProcessedAt = now
		s.ProgMux.Unlock()
	case xe.RepoInfo != nil:
		eventsProcessedCounter.WithLabelValues("repo_info", s.SocketURL).Inc()
	case xe.RepoMigrate != nil:
		eventsProcessedCounter.WithLabelValues("repo_migrate", s.SocketURL).Inc()
		now := time.Now()
		s.ProgMux.Lock()
		s.Progress.LastSeq = xe.RepoMigrate.Seq
		s.Progress.LastSeqProcessedAt = time.Now()
		s.ProgMux.Unlock()
		// Parse time from the event time string
		t, err := time.Parse(time.RFC3339, xe.RepoMigrate.Time)
		if err != nil {
			s.Logger.Error("error parsing time", "err", err)
			return nil
		}
		lastEvtCreatedAtGauge.WithLabelValues(s.SocketURL).Set(float64(t.UnixNano()))
		lastEvtProcessedAtGauge.WithLabelValues(s.SocketURL).Set(float64(now.UnixNano()))
		lastEvtCreatedEvtProcessedGapGauge.WithLabelValues(s.SocketURL).Set(float64(now.Sub(t).Seconds()))
		lastSeqGauge.WithLabelValues(s.SocketURL).Set(float64(xe.RepoHandle.Seq))
	case xe.RepoTombstone != nil:
		eventsProcessedCounter.WithLabelValues("repo_tombstone", s.SocketURL).Inc()
	case xe.LabelInfo != nil:
		eventsProcessedCounter.WithLabelValues("label_info", s.SocketURL).Inc()
	case xe.LabelLabels != nil:
		eventsProcessedCounter.WithLabelValues("label_labels", s.SocketURL).Inc()
	case xe.Error != nil:
		eventsProcessedCounter.WithLabelValues("error", s.SocketURL).Inc()
	}
	return nil
}

func (s *Sonar) HandleRepoCommit(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Commit) error {
	ctx, span := otel.Tracer("sonar").Start(ctx, "HandleRepoCommit")
	defer span.End()

	processedAt := time.Now()

	s.ProgMux.Lock()
	s.Progress.LastSeq = evt.Seq
	s.Progress.LastSeqProcessedAt = processedAt
	s.ProgMux.Unlock()

	lastSeqGauge.WithLabelValues(s.SocketURL).Set(float64(evt.Seq))

	log := s.Logger.With("repo", evt.Repo, "seq", evt.Seq, "commit", evt.Commit)

	rr, err := repo.ReadRepoFromCar(ctx, bytes.NewReader(evt.Blocks))
	if err != nil {
		s.Logger.Error("failed to read repo from car", "err", err)
		return nil
	}

	if evt.Rebase {
		log.Debug("rebase")
		rebasesProcessedCounter.WithLabelValues(s.SocketURL).Inc()
	}

	// Parse time from the event time string
	evtCreatedAt, err := time.Parse(time.RFC3339, evt.Time)
	if err != nil {
		s.Logger.Error("error parsing time", "err", err)
		return nil
	}

	lastEvtCreatedAtGauge.WithLabelValues(s.SocketURL).Set(float64(evtCreatedAt.UnixNano()))
	lastEvtProcessedAtGauge.WithLabelValues(s.SocketURL).Set(float64(processedAt.UnixNano()))
	lastEvtCreatedEvtProcessedGapGauge.WithLabelValues(s.SocketURL).Set(float64(processedAt.Sub(evtCreatedAt).Seconds()))

	for _, op := range evt.Ops {
		collection := strings.Split(op.Path, "/")[0]

		ek := repomgr.EventKind(op.Action)
		log = log.With("action", op.Action, "collection", collection)

		opsProcessedCounter.WithLabelValues(op.Action, collection, s.SocketURL).Inc()

		switch ek {
		case repomgr.EvtKindCreateRecord, repomgr.EvtKindUpdateRecord:
			// Grab the record from the merkel tree
			rc, rec, err := rr.GetRecord(ctx, op.Path)
			if err != nil {
				e := fmt.Errorf("getting record %s (%s) within seq %d for %s: %w", op.Path, *op.Cid, evt.Seq, evt.Repo, err)
				s.Logger.Error("failed to get a record from the event", "err", e)
				break
			}

			// Verify that the record cid matches the cid in the event
			if lexutil.LexLink(rc) != *op.Cid {
				e := fmt.Errorf("mismatch in record and op cid: %s != %s", rc, *op.Cid)
				s.Logger.Error("failed to LexLink the record in the event", "err", e)
				break
			}

			labelValues := []string{op.Action, s.SocketURL}

			var recCreatedAt time.Time
			var parseError error

			// Unpack the record and process it
			switch rec := rec.(type) {
			case *bsky.FeedPost:
				labelValues = append(labelValues, "feed_post")
				if rec.Embed != nil && rec.Embed.EmbedRecord != nil && rec.Embed.EmbedRecord.Record != nil {
					quoteRepostsProcessedCounter.WithLabelValues(s.SocketURL).Inc()
				}
				recCreatedAt, parseError = dateparse.ParseAny(rec.CreatedAt)
			case *bsky.FeedLike:
				labelValues = append(labelValues, "feed_like")
				recCreatedAt, parseError = dateparse.ParseAny(rec.CreatedAt)
			case *bsky.FeedRepost:
				labelValues = append(labelValues, "feed_repost")
				recCreatedAt, parseError = dateparse.ParseAny(rec.CreatedAt)
			case *bsky.GraphBlock:
				labelValues = append(labelValues, "graph_block")
				recCreatedAt, parseError = dateparse.ParseAny(rec.CreatedAt)
			case *bsky.GraphFollow:
				labelValues = append(labelValues, "graph_follow")
				recCreatedAt, parseError = dateparse.ParseAny(rec.CreatedAt)
			case *bsky.ActorProfile:
				labelValues = append(labelValues, "actor_profile")
			case *bsky.FeedGenerator:
				labelValues = append(labelValues, "feed_generator")
				recCreatedAt, parseError = dateparse.ParseAny(rec.CreatedAt)
			case *bsky.GraphList:
				labelValues = append(labelValues, "graph_list")
				recCreatedAt, parseError = dateparse.ParseAny(rec.CreatedAt)
			case *bsky.GraphListitem:
				labelValues = append(labelValues, "graph_listitem")
				recCreatedAt, parseError = dateparse.ParseAny(rec.CreatedAt)
			case *bsky.FeedThreadgate:
				labelValues = append(labelValues, "feed_threadgate")
				recCreatedAt, parseError = dateparse.ParseAny(rec.CreatedAt)
			case *bsky.LabelerService:
				labelValues = append(labelValues, "labeler_service")
			default:
				log.Warn("unknown record type", "rec", rec)
			}

			if len(labelValues) == 2 {
				labelValues = append(labelValues, "unknown")
			}

			recordsProcessedCounter.WithLabelValues(labelValues...).Inc()
			if parseError != nil {
				s.Logger.Error("error parsing time", "err", parseError)
				continue
			}
			if !recCreatedAt.IsZero() {
				lastEvtCreatedAtGauge.WithLabelValues(s.SocketURL).Set(float64(recCreatedAt.UnixNano()))
				lastEvtCreatedRecordCreatedGapGauge.WithLabelValues(s.SocketURL).Set(float64(evtCreatedAt.Sub(recCreatedAt).Seconds()))
				lastRecordCreatedEvtProcessedGapGauge.WithLabelValues(s.SocketURL).Set(float64(processedAt.Sub(recCreatedAt).Seconds()))
			}

		case repomgr.EvtKindDeleteRecord:
		default:
			s.Logger.Warn("unknown event kind from op action", "action", op.Action)
		}
	}

	eventProcessingDurationHistogram.WithLabelValues(s.SocketURL).Observe(time.Since(processedAt).Seconds())
	return nil
}
