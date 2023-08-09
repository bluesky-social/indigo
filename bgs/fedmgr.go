package bgs

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/events/schedulers/autoscaling"
	"github.com/bluesky-social/indigo/models"
	"go.opentelemetry.io/otel"

	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

type IndexCallback func(context.Context, *models.PDS, *events.XRPCStreamEvent) error

// TODO: rename me
type Slurper struct {
	cb IndexCallback

	db *gorm.DB

	lk     sync.Mutex
	active map[string]*activeSub

	newSubsDisabled bool

	shutdownChan   chan bool
	shutdownResult chan []error

	ssl bool
}

type activeSub struct {
	pds    *models.PDS
	lk     sync.RWMutex
	ctx    context.Context
	cancel func()
}

func NewSlurper(db *gorm.DB, cb IndexCallback, ssl bool) (*Slurper, error) {
	db.AutoMigrate(&SlurpConfig{})
	s := &Slurper{
		cb:             cb,
		db:             db,
		active:         make(map[string]*activeSub),
		ssl:            ssl,
		shutdownChan:   make(chan bool),
		shutdownResult: make(chan []error),
	}
	if err := s.loadConfig(); err != nil {
		return nil, err
	}

	// Start a goroutine to flush cursors to the DB every 30s
	go func() {
		for {
			select {
			case <-s.shutdownChan:
				log.Info("flushing PDS cursors on shutdown")
				ctx := context.Background()
				ctx, span := otel.Tracer("feedmgr").Start(ctx, "CursorFlusherShutdown")
				defer span.End()
				var errs []error
				if errs = s.flushCursors(ctx); len(errs) > 0 {
					for _, err := range errs {
						log.Errorf("failed to flush cursors on shutdown: %s", err)
					}
				}
				log.Info("done flushing PDS cursors on shutdown")
				s.shutdownResult <- errs
				return
			case <-time.After(time.Second * 10):
				log.Debug("flushing PDS cursors")
				ctx := context.Background()
				ctx, span := otel.Tracer("feedmgr").Start(ctx, "CursorFlusher")
				defer span.End()
				if errs := s.flushCursors(ctx); len(errs) > 0 {
					for _, err := range errs {
						log.Errorf("failed to flush cursors: %s", err)
					}
				}
				log.Debug("done flushing PDS cursors")
			}
		}
	}()

	return s, nil
}

// Shutdown shuts down the slurper
func (s *Slurper) Shutdown() []error {
	s.shutdownChan <- true
	log.Info("waiting for slurper shutdown")
	errs := <-s.shutdownResult
	if len(errs) > 0 {
		for _, err := range errs {
			log.Errorf("shutdown error: %s", err)
		}
	}
	log.Info("slurper shutdown complete")
	return errs
}

func (s *Slurper) loadConfig() error {
	var sc SlurpConfig
	if err := s.db.Find(&sc).Error; err != nil {
		return err
	}

	if sc.ID == 0 {
		if err := s.db.Create(&SlurpConfig{}).Error; err != nil {
			return err
		}
	}

	s.newSubsDisabled = sc.NewSubsDisabled

	return nil
}

type SlurpConfig struct {
	gorm.Model

	NewSubsDisabled bool
}

func (s *Slurper) SetNewSubsDisabled(dis bool) error {
	s.lk.Lock()
	defer s.lk.Unlock()

	if err := s.db.Model(SlurpConfig{}).Where("id = 1").Update("new_subs_disabled", dis).Error; err != nil {
		return err
	}

	s.newSubsDisabled = dis
	return nil
}

func (s *Slurper) GetNewSubsDisabledState() bool {
	s.lk.Lock()
	defer s.lk.Unlock()
	return s.newSubsDisabled
}

var ErrNewSubsDisabled = fmt.Errorf("new subscriptions temporarily disabled")

func (s *Slurper) SubscribeToPds(ctx context.Context, host string, reg bool) error {
	// TODO: for performance, lock on the hostname instead of global
	s.lk.Lock()
	defer s.lk.Unlock()
	if s.newSubsDisabled {
		return ErrNewSubsDisabled
	}

	_, ok := s.active[host]
	if ok {
		return nil
	}

	var peering models.PDS
	if err := s.db.Find(&peering, "host = ?", host).Error; err != nil {
		return err
	}

	if peering.Blocked {
		return fmt.Errorf("cannot subscribe to blocked pds")
	}

	if peering.ID == 0 {
		// New PDS!
		npds := models.PDS{
			Host:       host,
			SSL:        s.ssl,
			Registered: reg,
		}
		if err := s.db.Create(&npds).Error; err != nil {
			return err
		}

		peering = npds
	}

	if !peering.Registered && reg {
		peering.Registered = true
		if err := s.db.Model(models.PDS{}).Where("id = ?", peering.ID).Update("registered", true).Error; err != nil {
			return err
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	sub := activeSub{
		pds:    &peering,
		ctx:    ctx,
		cancel: cancel,
	}
	s.active[host] = &sub

	go s.subscribeWithRedialer(ctx, &peering, &sub)

	return nil
}

func (s *Slurper) RestartAll() error {
	s.lk.Lock()
	defer s.lk.Unlock()

	var all []models.PDS
	if err := s.db.Find(&all, "registered = true AND blocked = false").Error; err != nil {
		return err
	}

	for _, pds := range all {
		pds := pds

		ctx, cancel := context.WithCancel(context.Background())
		sub := activeSub{
			pds:    &pds,
			ctx:    ctx,
			cancel: cancel,
		}
		s.active[pds.Host] = &sub
		go s.subscribeWithRedialer(ctx, &pds, &sub)
	}

	return nil
}

func (s *Slurper) subscribeWithRedialer(ctx context.Context, host *models.PDS, sub *activeSub) {
	defer func() {
		s.lk.Lock()
		defer s.lk.Unlock()

		delete(s.active, host.Host)
	}()

	d := websocket.Dialer{}

	protocol := "ws"
	if s.ssl {
		protocol = "wss"
	}

	cursor := host.Cursor

	var backoff int
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		url := fmt.Sprintf("%s://%s/xrpc/com.atproto.sync.subscribeRepos?cursor=%d", protocol, host.Host, cursor)
		con, res, err := d.DialContext(ctx, url, nil)
		if err != nil {
			log.Warnw("dialing failed", "host", host.Host, "err", err, "backoff", backoff)
			time.Sleep(sleepForBackoff(backoff))
			backoff++

			if backoff > 15 {
				log.Warnw("pds does not appear to be online, disabling for now", "host", host.Host)
				if err := s.db.Model(&models.PDS{}).Where("id = ?", host.ID).Update("registered", false).Error; err != nil {
					log.Errorf("failed to unregister failing pds: %w", err)
				}

				return
			}

			continue
		}

		log.Info("event subscription response code: ", res.StatusCode)

		if err := s.handleConnection(ctx, host, con, &cursor, sub); err != nil {
			if errors.Is(err, ErrTimeoutShutdown) {
				log.Infof("shutting down pds subscription to %s, no activity after %s", host.Host, EventsTimeout)
				return
			}
			log.Warnf("connection to %q failed: %s", host.Host, err)
		}
	}
}

func sleepForBackoff(b int) time.Duration {
	if b == 0 {
		return 0
	}

	if b < 10 {
		return (time.Duration(b) * 2) + (time.Millisecond * time.Duration(rand.Intn(1000)))
	}

	return time.Second * 30
}

var ErrTimeoutShutdown = fmt.Errorf("timed out waiting for new events")

var EventsTimeout = time.Minute

func (s *Slurper) handleConnection(ctx context.Context, host *models.PDS, con *websocket.Conn, lastCursor *int64, sub *activeSub) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	rsc := &events.RepoStreamCallbacks{
		RepoCommit: func(evt *comatproto.SyncSubscribeRepos_Commit) error {
			log.Debugw("got remote repo event", "host", host.Host, "repo", evt.Repo, "seq", evt.Seq)
			if err := s.cb(context.TODO(), host, &events.XRPCStreamEvent{
				RepoCommit: evt,
			}); err != nil {
				log.Errorf("failed handling event from %q (%d): %s", host.Host, evt.Seq, err)
			}
			*lastCursor = evt.Seq

			if err := s.updateCursor(sub, *lastCursor); err != nil {
				return fmt.Errorf("updating cursor: %w", err)
			}

			return nil
		},
		RepoHandle: func(evt *comatproto.SyncSubscribeRepos_Handle) error {
			log.Infow("got remote handle update event", "host", host.Host, "did", evt.Did, "handle", evt.Handle)
			if err := s.cb(context.TODO(), host, &events.XRPCStreamEvent{
				RepoHandle: evt,
			}); err != nil {
				log.Errorf("failed handling event from %q (%d): %s", host.Host, evt.Seq, err)
			}
			*lastCursor = evt.Seq

			if err := s.updateCursor(sub, *lastCursor); err != nil {
				return fmt.Errorf("updating cursor: %w", err)
			}

			return nil
		},
		RepoMigrate: func(evt *comatproto.SyncSubscribeRepos_Migrate) error {
			log.Infow("got remote repo migrate event", "host", host.Host, "did", evt.Did, "migrateTo", evt.MigrateTo)
			if err := s.cb(context.TODO(), host, &events.XRPCStreamEvent{
				RepoMigrate: evt,
			}); err != nil {
				log.Errorf("failed handling event from %q (%d): %s", host.Host, evt.Seq, err)
			}
			*lastCursor = evt.Seq

			if err := s.updateCursor(sub, *lastCursor); err != nil {
				return fmt.Errorf("updating cursor: %w", err)
			}

			return nil
		},
		RepoTombstone: func(evt *comatproto.SyncSubscribeRepos_Tombstone) error {
			log.Infow("got remote repo tombstone event", "host", host.Host, "did", evt.Did)
			if err := s.cb(context.TODO(), host, &events.XRPCStreamEvent{
				RepoTombstone: evt,
			}); err != nil {
				log.Errorf("failed handling event from %q (%d): %s", host.Host, evt.Seq, err)
			}
			*lastCursor = evt.Seq

			if err := s.updateCursor(sub, *lastCursor); err != nil {
				return fmt.Errorf("updating cursor: %w", err)
			}

			return nil
		},
		RepoInfo: func(info *comatproto.SyncSubscribeRepos_Info) error {
			log.Infow("info event", "name", info.Name, "message", info.Message, "host", host.Host)
			return nil
		},
		// TODO: all the other event types (handle change, migration, etc)
		Error: func(errf *events.ErrorFrame) error {
			switch errf.Error {
			case "FutureCursor":
				// if we get a FutureCursor frame, reset our sequence number for this host
				if err := s.db.Table("pds").Where("id = ?", host.ID).Update("cursor", 0).Error; err != nil {
					return err
				}

				return fmt.Errorf("got FutureCursor frame, reset cursor tracking for host")
			default:
				return fmt.Errorf("error frame: %s: %s", errf.Error, errf.Message)
			}
		},
	}

	scalingSettings := autoscaling.AutoscaleSettings{
		Concurrency:              1,
		MaxConcurrency:           360,
		AutoscaleFrequency:       time.Second,
		ThroughputBucketCount:    60,
		ThroughputBucketDuration: time.Second,
	}

	pool := autoscaling.NewScheduler(scalingSettings, con.RemoteAddr().String(), rsc.EventHandler)
	return events.HandleRepoStream(ctx, con, pool)
}

func (s *Slurper) updateCursor(sub *activeSub, curs int64) error {
	sub.lk.Lock()
	defer sub.lk.Unlock()
	sub.pds.Cursor = curs
	return nil
}

type cursorSnapshot struct {
	id     uint
	cursor int64
}

// flushCursors updates the PDS cursors in the DB for all active subscriptions
func (s *Slurper) flushCursors(ctx context.Context) []error {
	ctx, span := otel.Tracer("feedmgr").Start(ctx, "flushCursors")
	defer span.End()

	var cursors []cursorSnapshot

	s.lk.Lock()
	// Iterate over active subs and copy the current cursor
	for _, sub := range s.active {
		sub.lk.RLock()
		cursors = append(cursors, cursorSnapshot{
			id:     sub.pds.ID,
			cursor: sub.pds.Cursor,
		})
		sub.lk.RUnlock()
	}
	s.lk.Unlock()

	errs := []error{}

	tx := s.db.WithContext(ctx).Begin()
	for _, cursor := range cursors {
		if err := tx.WithContext(ctx).Model(models.PDS{}).Where("id = ?", cursor.id).UpdateColumn("cursor", cursor.cursor).Error; err != nil {
			errs = append(errs, err)
		}
	}
	if err := tx.WithContext(ctx).Commit().Error; err != nil {
		errs = append(errs, err)
	}

	return errs
}

func (s *Slurper) GetActiveList() []string {
	s.lk.Lock()
	defer s.lk.Unlock()
	var out []string
	for k := range s.active {
		out = append(out, k)
	}

	return out
}

var ErrNoActiveConnection = fmt.Errorf("no active connection to host")

func (s *Slurper) KillUpstreamConnection(host string, block bool) error {
	s.lk.Lock()
	defer s.lk.Unlock()

	ac, ok := s.active[host]
	if !ok {
		return fmt.Errorf("killing connection %q: %w", host, ErrNoActiveConnection)
	}
	ac.cancel()

	if block {
		if err := s.db.Model(models.PDS{}).Where("id = ?", ac.pds.ID).UpdateColumn("blocked", true).Error; err != nil {
			return fmt.Errorf("failed to set host as blocked: %w", err)
		}
	}

	return nil
}
