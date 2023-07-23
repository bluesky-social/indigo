package sonar

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/api/bsky"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/util"
	"github.com/goccy/go-json"
	"github.com/labstack/gommon/log"

	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/repomgr"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap"
)

type Sonar struct {
	SocketURL  string
	Progress   *Progress
	ProgMux    sync.Mutex
	Logger     *zap.SugaredLogger
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

func NewSonar(logger *zap.SugaredLogger, cursorFile string, socketURL string) (*Sonar, error) {
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
		logger.Infof("cursor file does not exist, creating %s", cursorFile)
		// Create the cursor file
		err := s.WriteCursorFile()
		if err != nil {
			return nil, fmt.Errorf("failed to write cursor file: %+v", err)
		}
	} else {
		// Read the cursor file
		err := s.ReadCursorFile()
		if err != nil {
			logger.Errorf("read cursor file, will start drinking from live: %+v", err.Error())
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
			log.Errorf("error parsing time: %+v", err)
			return nil
		}
		lastSeqCommittedAtGauge.WithLabelValues(s.SocketURL).Set(float64(t.UnixNano()))
		lastSeqProcessedAtGauge.WithLabelValues(s.SocketURL).Set(float64(now.UnixNano()))
		lastSeqGauge.WithLabelValues(s.SocketURL).Set(float64(xe.RepoHandle.Seq))
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
			log.Errorf("error parsing time: %+v", err)
			return nil
		}
		lastSeqCommittedAtGauge.WithLabelValues(s.SocketURL).Set(float64(t.UnixNano()))
		lastSeqProcessedAtGauge.WithLabelValues(s.SocketURL).Set(float64(now.UnixNano()))
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

	start := time.Now()

	s.ProgMux.Lock()
	s.Progress.LastSeq = evt.Seq
	s.Progress.LastSeqProcessedAt = start
	s.ProgMux.Unlock()

	lastSeqGauge.WithLabelValues(s.SocketURL).Set(float64(evt.Seq))

	log := s.Logger.With("repo", evt.Repo, "seq", evt.Seq, "commit", evt.Commit)

	rr, err := repo.ReadRepoFromCar(ctx, bytes.NewReader(evt.Blocks))
	if err != nil {
		log.Errorf("failed to read repo from car: %+v\n", err)
		return nil
	}

	if evt.Rebase {
		log.Debug("rebase")
		rebasesProcessedCounter.WithLabelValues(s.SocketURL).Inc()
	}

	// Parse time from the event time string
	t, err := time.Parse(time.RFC3339, evt.Time)
	if err != nil {
		log.Errorf("error parsing time: %+v", err)
		return nil
	}

	lastSeqCommittedAtGauge.WithLabelValues(s.SocketURL).Set(float64(t.UnixNano()))
	lastSeqProcessedAtGauge.WithLabelValues(s.SocketURL).Set(float64(start.UnixNano()))

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
				log.Errorf("failed to get a record from the event: %+v\n", e)
				break
			}

			// Verify that the record cid matches the cid in the event
			if lexutil.LexLink(rc) != *op.Cid {
				e := fmt.Errorf("mismatch in record and op cid: %s != %s", rc, *op.Cid)
				log.Errorf("failed to LexLink the record in the event: %+v\n", e)
				break
			}

			// Unpack the record and process it
			switch rec := rec.(type) {
			case *bsky.FeedPost:
				recordsProcessedCounter.WithLabelValues("feed_post", s.SocketURL).Inc()
				if rec.Embed != nil && rec.Embed.EmbedRecord != nil && rec.Embed.EmbedRecord.Record != nil {
					quoteRepostsProcessedCounter.WithLabelValues(s.SocketURL).Inc()
				}
				// Parse time from the event time string
				recCreatedAt, err := util.ParseTimestamp(rec.CreatedAt)
				if err != nil {
					log.Errorf("error parsing time: %+v", err)
					continue
				}
				lastSeqCreatedAtGauge.WithLabelValues(s.SocketURL).Set(float64(recCreatedAt.UnixNano()))
			case *bsky.FeedLike:
				recordsProcessedCounter.WithLabelValues("feed_like", s.SocketURL).Inc()
				// Parse time from the event time string
				recCreatedAt, err := util.ParseTimestamp(rec.CreatedAt)
				if err != nil {
					log.Errorf("error parsing time: %+v", err)
					continue
				}
				lastSeqCreatedAtGauge.WithLabelValues(s.SocketURL).Set(float64(recCreatedAt.UnixNano()))
			case *bsky.FeedRepost:
				recordsProcessedCounter.WithLabelValues("feed_repost", s.SocketURL).Inc()
				// Parse time from the event time string
				recCreatedAt, err := util.ParseTimestamp(rec.CreatedAt)
				if err != nil {
					log.Errorf("error parsing time: %+v", err)
					continue
				}
				lastSeqCreatedAtGauge.WithLabelValues(s.SocketURL).Set(float64(recCreatedAt.UnixNano()))
			case *bsky.GraphBlock:
				recordsProcessedCounter.WithLabelValues("graph_block", s.SocketURL).Inc()
				// Parse time from the event time string
				recCreatedAt, err := util.ParseTimestamp(rec.CreatedAt)
				if err != nil {
					log.Errorf("error parsing time: %+v", err)
					continue
				}
				lastSeqCreatedAtGauge.WithLabelValues(s.SocketURL).Set(float64(recCreatedAt.UnixNano()))
			case *bsky.GraphFollow:
				recordsProcessedCounter.WithLabelValues("graph_follow", s.SocketURL).Inc()
				// Parse time from the event time string
				recCreatedAt, err := util.ParseTimestamp(rec.CreatedAt)
				if err != nil {
					log.Errorf("error parsing time: %+v", err)
					continue
				}
				lastSeqCreatedAtGauge.WithLabelValues(s.SocketURL).Set(float64(recCreatedAt.UnixNano()))
			case *bsky.ActorProfile:
				recordsProcessedCounter.WithLabelValues("actor_profile", s.SocketURL).Inc()
			case *bsky.FeedGenerator:
				recordsProcessedCounter.WithLabelValues("feed_generator", s.SocketURL).Inc()
				// Parse time from the event time string
				recCreatedAt, err := util.ParseTimestamp(rec.CreatedAt)
				if err != nil {
					log.Errorf("error parsing time: %+v", err)
					continue
				}
				lastSeqCreatedAtGauge.WithLabelValues(s.SocketURL).Set(float64(recCreatedAt.UnixNano()))
			case *bsky.GraphList:
				recordsProcessedCounter.WithLabelValues("graph_list", s.SocketURL).Inc()
				// Parse time from the event time string
				recCreatedAt, err := util.ParseTimestamp(rec.CreatedAt)
				if err != nil {
					log.Errorf("error parsing time: %+v", err)
					continue
				}
				lastSeqCreatedAtGauge.WithLabelValues(s.SocketURL).Set(float64(recCreatedAt.UnixNano()))
			case *bsky.GraphListitem:
				recordsProcessedCounter.WithLabelValues("graph_listitem", s.SocketURL).Inc()
				// Parse time from the event time string
				recCreatedAt, err := util.ParseTimestamp(rec.CreatedAt)
				if err != nil {
					log.Errorf("error parsing time: %+v", err)
					continue
				}
				lastSeqCreatedAtGauge.WithLabelValues(s.SocketURL).Set(float64(recCreatedAt.UnixNano()))
			default:
				log.Warnf("unknown record type: %+v", rec)
			}

		case repomgr.EvtKindDeleteRecord:
		default:
			log.Warnf("unknown event kind from op action: %+v", op.Action)
		}
	}

	eventProcessingDurationHistogram.WithLabelValues(s.SocketURL).Observe(time.Since(start).Seconds())
	return nil
}
