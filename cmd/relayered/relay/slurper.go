package relay

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/cmd/relayered/relay/models"
	"github.com/bluesky-social/indigo/cmd/relayered/stream"
	"github.com/bluesky-social/indigo/cmd/relayered/stream/schedulers/parallel"

	"github.com/RussellLuo/slidingwindow"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

type ProcessMessageFunc func(ctx context.Context, evt *stream.XRPCStreamEvent, hostname string, hostID uint64) error

type Slurper struct {
	cb     ProcessMessageFunc
	db     *gorm.DB
	Config *SlurperConfig

	lk     sync.Mutex
	active map[string]*Subscription

	LimitMtx sync.RWMutex
	Limiters map[uint64]*Limiters

	NewHostPerDayLimiter *slidingwindow.Limiter

	shutdownChan   chan bool
	shutdownResult chan []error

	logger *slog.Logger
}

type Limiters struct {
	PerSecond *slidingwindow.Limiter
	PerHour   *slidingwindow.Limiter
	PerDay    *slidingwindow.Limiter
}

type SlurperConfig struct {
	SSL                   bool
	DefaultPerSecondLimit int64
	DefaultPerHourLimit   int64
	DefaultPerDayLimit    int64
	DefaultRepoLimit      int64
	ConcurrencyPerHost    int64
	MaxQueuePerHost       int64
	NewHostPerDayLimit    int64
}

func DefaultSlurperConfig() *SlurperConfig {
	return &SlurperConfig{
		SSL:                   false,
		DefaultPerSecondLimit: 50,
		DefaultPerHourLimit:   2500,
		DefaultPerDayLimit:    20_000,
		DefaultRepoLimit:      100,
		ConcurrencyPerHost:    100,
		MaxQueuePerHost:       1_000,
	}
}

// represents an active client connection to a remote host
type Subscription struct {
	Hostname string
	HostID   uint64
	LastSeq  int64 // XXX: switch to an atomic
	Limiters *Limiters

	lk     sync.RWMutex
	ctx    context.Context
	cancel func()
}

func (sub *Subscription) UpdateSeq(seq int64) {
	sub.lk.Lock()
	defer sub.lk.Unlock()
	sub.LastSeq = seq
}

func NewSlurper(db *gorm.DB, cb ProcessMessageFunc, config *SlurperConfig, logger *slog.Logger) (*Slurper, error) {
	if config == nil {
		config = DefaultSlurperConfig()
	}
	if logger == nil {
		logger = slog.Default()
	}

	// NOTE: discarded second argument is not an `error` type
	newHostPerDayLimiter, _ := slidingwindow.NewLimiter(time.Hour*24, config.NewHostPerDayLimit, windowFunc)

	s := &Slurper{
		cb:                   cb,
		db:                   db,
		Config:               config,
		active:               make(map[string]*Subscription),
		Limiters:             make(map[uint64]*Limiters),
		shutdownChan:         make(chan bool),
		shutdownResult:       make(chan []error),
		NewHostPerDayLimiter: newHostPerDayLimiter,
		logger:               logger,
	}

	// Start a goroutine to flush cursors to the DB every 30s
	go func() {
		for {
			select {
			case <-s.shutdownChan:
				s.logger.Info("flushing Host cursors on shutdown")
				ctx := context.Background()
				var errs []error
				if errs = s.flushCursors(ctx); len(errs) > 0 {
					for _, err := range errs {
						s.logger.Error("failed to flush cursors on shutdown", "err", err)
					}
				}
				s.logger.Info("done flushing Host cursors on shutdown")
				s.shutdownResult <- errs
				return
			case <-time.After(time.Second * 10):
				s.logger.Debug("flushing Host cursors")
				ctx := context.Background()
				if errs := s.flushCursors(ctx); len(errs) > 0 {
					for _, err := range errs {
						s.logger.Error("failed to flush cursors", "err", err)
					}
				}
				s.logger.Debug("done flushing Host cursors")
			}
		}
	}()

	return s, nil
}

func windowFunc() (slidingwindow.Window, slidingwindow.StopFunc) {
	return slidingwindow.NewLocalWindow()
}

func (s *Slurper) GetLimiters(hostID uint64) *Limiters {
	s.LimitMtx.RLock()
	defer s.LimitMtx.RUnlock()
	return s.Limiters[hostID]
}

/*
XXX

	func (s *Slurper) GetOrCreateLimiters(pdsID uint64, perSecLimit int64, perHourLimit int64, perDayLimit int64) *Limiters {
		s.LimitMtx.RLock()
		defer s.LimitMtx.RUnlock()
		lim, ok := s.Limiters[pdsID]
		if !ok {
			perSec, _ := slidingwindow.NewLimiter(time.Second, perSecLimit, windowFunc)
			perHour, _ := slidingwindow.NewLimiter(time.Hour, perHourLimit, windowFunc)
			perDay, _ := slidingwindow.NewLimiter(time.Hour*24, perDayLimit, windowFunc)
			lim = &Limiters{
				PerSecond: perSec,
				PerHour:   perHour,
				PerDay:    perDay,
			}
			s.Limiters[pdsID] = lim
		}

		return lim
	}
*/
func (s *Slurper) GetOrCreateLimiters(hostID uint64) *Limiters {
	s.LimitMtx.RLock()
	defer s.LimitMtx.RUnlock()
	lim, ok := s.Limiters[hostID]
	if !ok {
		perSec, _ := slidingwindow.NewLimiter(time.Second, s.Config.DefaultPerSecondLimit, windowFunc)
		perHour, _ := slidingwindow.NewLimiter(time.Hour, s.Config.DefaultPerHourLimit, windowFunc)
		perDay, _ := slidingwindow.NewLimiter(time.Hour*24, s.Config.DefaultPerDayLimit, windowFunc)
		lim = &Limiters{
			PerSecond: perSec,
			PerHour:   perHour,
			PerDay:    perDay,
		}
		s.Limiters[hostID] = lim
	}

	return lim
}

func (s *Slurper) SetLimits(hostID uint64, perSecLimit int64, perHourLimit int64, perDayLimit int64) {
	s.LimitMtx.Lock()
	defer s.LimitMtx.Unlock()
	lim, ok := s.Limiters[hostID]
	if !ok {
		perSec, _ := slidingwindow.NewLimiter(time.Second, perSecLimit, windowFunc)
		perHour, _ := slidingwindow.NewLimiter(time.Hour, perHourLimit, windowFunc)
		perDay, _ := slidingwindow.NewLimiter(time.Hour*24, perDayLimit, windowFunc)
		lim = &Limiters{
			PerSecond: perSec,
			PerHour:   perHour,
			PerDay:    perDay,
		}
		s.Limiters[hostID] = lim
	}

	lim.PerSecond.SetLimit(perSecLimit)
	lim.PerHour.SetLimit(perHourLimit)
	lim.PerDay.SetLimit(perDayLimit)
}

// Shutdown shuts down the slurper
func (s *Slurper) Shutdown() []error {
	s.shutdownChan <- true
	s.logger.Info("waiting for slurper shutdown")
	errs := <-s.shutdownResult
	if len(errs) > 0 {
		for _, err := range errs {
			s.logger.Error("shutdown error", "err", err)
		}
	}
	s.logger.Info("slurper shutdown complete")
	return errs
}

func (s *Slurper) CheckIfSubscribed(hostname string) bool {
	s.lk.Lock()
	defer s.lk.Unlock()

	_, ok := s.active[hostname]
	return ok
}

func (s *Slurper) Subscribe(host *models.Host, newHost bool) error {
	// TODO: for performance, lock on the hostname instead of global
	s.lk.Lock()
	defer s.lk.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	sub := Subscription{
		Hostname: host.Hostname,
		HostID:   host.ID,
		ctx:      ctx,
		cancel:   cancel,
	}
	s.active[host.Hostname] = &sub

	s.GetOrCreateLimiters(host.ID)

	go s.subscribeWithRedialer(ctx, host, &sub, newHost)

	return nil
}

func (s *Slurper) subscribeWithRedialer(ctx context.Context, host *models.Host, sub *Subscription, newHost bool) {
	defer func() {
		s.lk.Lock()
		defer s.lk.Unlock()

		delete(s.active, host.Hostname)
	}()

	d := websocket.Dialer{
		HandshakeTimeout: time.Second * 5,
	}

	// cursor by 200 events to smooth over unclean shutdowns
	if host.LastSeq > 200 {
		host.LastSeq -= 200
	} else {
		host.LastSeq = 0
	}

	cursor := host.LastSeq

	connectedInbound.Inc()
	defer connectedInbound.Dec()
	// TODO:? maybe keep a gauge of 'in retry backoff' sources?

	var backoff int
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		u := host.SubscribeReposURL()
		if newHost {
			u = fmt.Sprintf("%s?cursor=%d", u, cursor)
		}
		conn, res, err := d.DialContext(ctx, u, nil)
		if err != nil {
			s.logger.Warn("dialing failed", "host", host.Hostname, "err", err, "backoff", backoff)
			time.Sleep(sleepForBackoff(backoff))
			backoff++

			if backoff > 15 {
				s.logger.Warn("host does not appear to be online, disabling for now", "host", host.Hostname)
				if err := s.db.Model(&models.Host{}).Where("id = ?", host.ID).Update("registered", false).Error; err != nil {
					s.logger.Error("failed to unregister failing host", "err", err)
				}

				return
			}

			continue
		}

		s.logger.Info("event subscription response", "code", res.StatusCode, "url", u)

		curCursor := cursor
		if err := s.handleConnection(ctx, conn, &cursor, sub); err != nil {
			if errors.Is(err, ErrTimeoutShutdown) {
				s.logger.Info("shutting down host subscription after timeout", "host", host.Hostname, "time", EventsTimeout)
				return
			}
			s.logger.Warn("connection to failed", "host", host.Hostname, "err", err)
			// TODO: measure the last N connection error times and if they're coming too fast reconnect slower or don't reconnect and wait for requestCrawl
		}

		if cursor > curCursor {
			backoff = 0
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

var EventsTimeout = time.Minute

func (s *Slurper) handleConnection(ctx context.Context, conn *websocket.Conn, lastCursor *int64, sub *Subscription) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	rsc := &stream.RepoStreamCallbacks{
		RepoCommit: func(evt *comatproto.SyncSubscribeRepos_Commit) error {
			logger := s.logger.With("host", sub.Hostname, "did", evt.Repo, "seq", evt.Seq, "eventType", "commit")
			logger.Debug("got remote repo event")
			if err := s.cb(context.TODO(), &stream.XRPCStreamEvent{RepoCommit: evt}, sub.Hostname, sub.HostID); err != nil {
				logger.Error("failed handling event", "err", err)
			}
			*lastCursor = evt.Seq

			sub.UpdateSeq(*lastCursor)

			return nil
		},
		RepoSync: func(evt *comatproto.SyncSubscribeRepos_Sync) error {
			logger := s.logger.With("host", sub.Hostname, "did", evt.Did, "seq", evt.Seq, "eventType", "sync")
			logger.Debug("got remote repo event")
			if err := s.cb(context.TODO(), &stream.XRPCStreamEvent{RepoSync: evt}, sub.Hostname, sub.HostID); err != nil {
				s.logger.Error("failed handling event", "err", err)
			}
			*lastCursor = evt.Seq

			sub.UpdateSeq(*lastCursor)

			return nil
		},
		RepoIdentity: func(evt *comatproto.SyncSubscribeRepos_Identity) error {
			logger := s.logger.With("host", sub.Hostname, "did", evt.Did, "seq", evt.Seq, "eventType", "identity")
			logger.Debug("identity event")
			if err := s.cb(context.TODO(), &stream.XRPCStreamEvent{RepoIdentity: evt}, sub.Hostname, sub.HostID); err != nil {
				logger.Error("failed handling event", "err", err)
			}
			*lastCursor = evt.Seq

			sub.UpdateSeq(*lastCursor)

			return nil
		},
		RepoAccount: func(evt *comatproto.SyncSubscribeRepos_Account) error {
			logger := s.logger.With("host", sub.Hostname, "did", evt.Did, "seq", evt.Seq, "eventType", "account")
			s.logger.Debug("account event")
			if err := s.cb(context.TODO(), &stream.XRPCStreamEvent{RepoAccount: evt}, sub.Hostname, sub.HostID); err != nil {
				logger.Error("failed handling event", "err", err)
			}
			*lastCursor = evt.Seq

			sub.UpdateSeq(*lastCursor)

			return nil
		},
		Error: func(evt *stream.ErrorFrame) error {
			switch evt.Error {
			case "FutureCursor":
				// XXX: need test coverage for this path
				// if we get a FutureCursor frame, reset our sequence number for this host
				if err := s.db.Table("host").Where("id = ?", sub.HostID).Update("last_seq", 0).Error; err != nil {
					return err
				}

				*lastCursor = 0
				return fmt.Errorf("got FutureCursor frame, reset cursor tracking for host")
			default:
				return fmt.Errorf("error frame: %s: %s", evt.Error, evt.Message)
			}
		},
		RepoInfo: func(info *comatproto.SyncSubscribeRepos_Info) error {
			s.logger.Debug("info event", "name", info.Name, "message", info.Message, "host", sub.Hostname)
			return nil
		},
		RepoHandle: func(evt *comatproto.SyncSubscribeRepos_Handle) error { // DEPRECATED
			logger := s.logger.With("host", sub.Hostname, "did", evt.Did, "seq", evt.Seq, "eventType", "handle")
			logger.Debug("got remote handle update event", "handle", evt.Handle)
			if err := s.cb(context.TODO(), &stream.XRPCStreamEvent{RepoHandle: evt}, sub.Hostname, sub.HostID); err != nil {
				logger.Error("failed handling event", "err", err)
			}
			*lastCursor = evt.Seq

			sub.UpdateSeq(*lastCursor)

			return nil
		},
		RepoMigrate: func(evt *comatproto.SyncSubscribeRepos_Migrate) error { // DEPRECATED
			logger := s.logger.With("host", sub.Hostname, "did", evt.Did, "seq", evt.Seq, "eventType", "migrate")
			logger.Debug("got remote repo migrate event", "migrateTo", evt.MigrateTo)
			if err := s.cb(context.TODO(), &stream.XRPCStreamEvent{RepoMigrate: evt}, sub.Hostname, sub.HostID); err != nil {
				logger.Error("failed handling event", "err", err)
			}
			*lastCursor = evt.Seq

			sub.UpdateSeq(*lastCursor)

			return nil
		},
		RepoTombstone: func(evt *comatproto.SyncSubscribeRepos_Tombstone) error { // DEPRECATED
			logger := s.logger.With("host", sub.Hostname, "did", evt.Did, "seq", evt.Seq, "eventType", "tombstone")
			logger.Debug("got remote repo tombstone event")
			if err := s.cb(context.TODO(), &stream.XRPCStreamEvent{RepoTombstone: evt}, sub.Hostname, sub.HostID); err != nil {
				logger.Error("failed handling event", "err", err)
			}
			*lastCursor = evt.Seq

			sub.UpdateSeq(*lastCursor)

			return nil
		},
	}

	lims := s.GetOrCreateLimiters(sub.HostID)

	limiters := []*slidingwindow.Limiter{
		lims.PerSecond,
		lims.PerHour,
		lims.PerDay,
	}

	instrumentedRSC := stream.NewInstrumentedRepoStreamCallbacks(limiters, rsc.EventHandler)

	pool := parallel.NewScheduler(
		100,
		1_000,
		conn.RemoteAddr().String(),
		instrumentedRSC.EventHandler,
	)
	return stream.HandleRepoStream(ctx, conn, pool, nil)
}

type cursorSnapshot struct {
	hostID uint64
	cursor int64
}

// flushCursors updates the Host cursors in the DB for all active subscriptions
func (s *Slurper) flushCursors(ctx context.Context) []error {
	start := time.Now()
	//ctx, span := otel.Tracer("feedmgr").Start(ctx, "flushCursors")
	//defer span.End()

	var cursors []cursorSnapshot

	s.lk.Lock()
	// Iterate over active subs and copy the current cursor
	for _, sub := range s.active {
		sub.lk.RLock()
		cursors = append(cursors, cursorSnapshot{
			hostID: sub.HostID,
			cursor: sub.LastSeq,
		})
		sub.lk.RUnlock()
	}
	s.lk.Unlock()

	errs := []error{}
	okcount := 0

	tx := s.db.WithContext(ctx).Begin()
	for _, cursor := range cursors {
		if err := tx.WithContext(ctx).Model(models.Host{}).Where("id = ?", cursor.hostID).UpdateColumn("last_seq", cursor.cursor).Error; err != nil {
			errs = append(errs, err)
		} else {
			okcount++
		}
	}
	if err := tx.WithContext(ctx).Commit().Error; err != nil {
		errs = append(errs, err)
	}
	dt := time.Since(start)
	s.logger.Info("flushCursors", "dt", dt, "ok", okcount, "errs", len(errs))

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

func (s *Slurper) KillUpstreamConnection(hostname string, ban bool) error {
	s.lk.Lock()
	defer s.lk.Unlock()

	ac, ok := s.active[hostname]
	if !ok {
		return fmt.Errorf("killing connection %q: %w", hostname, ErrNoActiveConnection)
	}
	ac.cancel()
	// cleanup in the run thread subscribeWithRedialer() will delete(s.active, host)

	if ban {
		if err := s.db.Model(models.Host{}).Where("id = ?", ac.HostID).UpdateColumn("status", "banned").Error; err != nil {
			return fmt.Errorf("failed to set host as banned: %w", err)
		}
	}

	return nil
}
