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
	"github.com/bluesky-social/indigo/cmd/rerelay/relay/models"
	"github.com/bluesky-social/indigo/cmd/rerelay/stream"
	"github.com/bluesky-social/indigo/cmd/rerelay/stream/schedulers/parallel"

	"github.com/RussellLuo/slidingwindow"
	"github.com/gorilla/websocket"
)

type ProcessMessageFunc func(ctx context.Context, evt *stream.XRPCStreamEvent, hostname string, hostID uint64) error
type PersistCursorFunc func(ctx context.Context, cursors *[]HostCursor) error
type PersistHostStatusFunc func(ctx context.Context, hostID uint64, state models.HostStatus) error

// `Slurper` is the sub-system of the relay which manages active websocket firehose connections to upstream hosts.
//
// It enforces rate-limits, tracks cursors, and retries connections. It passes recieved messages on to the main relay via a callback function. `Slurper` does not talk to the database directly, but does have some callback to persist host state (cursors and hosting status for some error conditions).
type Slurper struct {
	processCallback ProcessMessageFunc
	Config          *SlurperConfig

	subsLk sync.Mutex
	subs   map[string]*Subscription

	LimitMtx sync.RWMutex
	Limiters map[uint64]*Limiters

	NewHostPerDayLimiter *slidingwindow.Limiter

	shutdownChan   chan bool
	shutdownResult chan error

	logger *slog.Logger
}

type Limiters struct {
	PerSecond *slidingwindow.Limiter
	PerHour   *slidingwindow.Limiter
	PerDay    *slidingwindow.Limiter
}

type SlurperConfig struct {
	SSL                       bool
	DefaultPerSecondLimit     int64
	DefaultPerHourLimit       int64
	DefaultPerDayLimit        int64
	DefaultRepoLimit          int64
	ConcurrencyPerHost        int64
	NewHostPerDayLimit        int64
	PersistCursorPeriod       time.Duration
	PersistCursorCallback     PersistCursorFunc
	PersistHostStatusCallback PersistHostStatusFunc
}

func DefaultSlurperConfig() *SlurperConfig {
	return &SlurperConfig{
		SSL:                   false,
		DefaultPerSecondLimit: 50,
		DefaultPerHourLimit:   2500,
		DefaultPerDayLimit:    20_000,
		DefaultRepoLimit:      100,
		ConcurrencyPerHost:    40,
		PersistCursorPeriod:   time.Second * 10,
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

func (sub *Subscription) HostCursor() HostCursor {
	sub.lk.Lock()
	defer sub.lk.Unlock()
	return HostCursor{
		HostID:  sub.HostID,
		LastSeq: sub.LastSeq,
	}
}

func NewSlurper(processCallback ProcessMessageFunc, config *SlurperConfig, logger *slog.Logger) (*Slurper, error) {
	if config == nil {
		config = DefaultSlurperConfig()
	}
	if logger == nil {
		logger = slog.Default()
	}

	// NOTE: discarded second argument is not an `error` type
	newHostPerDayLimiter, _ := slidingwindow.NewLimiter(time.Hour*24, config.NewHostPerDayLimit, windowFunc)

	s := &Slurper{
		processCallback:      processCallback,
		Config:               config,
		subs:                 make(map[string]*Subscription),
		Limiters:             make(map[uint64]*Limiters),
		shutdownChan:         make(chan bool),
		shutdownResult:       make(chan error),
		NewHostPerDayLimiter: newHostPerDayLimiter,
		logger:               logger,
	}

	// Start a goroutine to persist cursors (both periodically and and on shutdown)
	go func() {
		for {
			select {
			case <-s.shutdownChan:
				s.logger.Info("starting shutdown host cursor flush")
				s.shutdownResult <- s.persistCursors(context.Background())
				return
			case <-time.After(config.PersistCursorPeriod):
				if err := s.persistCursors(context.Background()); err != nil {
					s.logger.Error("failed to flush cursors", "err", err)
				}
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
func (s *Slurper) Shutdown() error {
	s.shutdownChan <- true
	s.logger.Info("waiting for slurper shutdown")
	err := <-s.shutdownResult
	if err != nil {
		s.logger.Error("shutdown error", "err", err)
	}
	s.logger.Info("slurper shutdown complete")
	return err
}

func (s *Slurper) CheckIfSubscribed(hostname string) bool {
	s.subsLk.Lock()
	defer s.subsLk.Unlock()

	_, ok := s.subs[hostname]
	return ok
}

func (s *Slurper) Subscribe(host *models.Host, newHost bool) error {
	// TODO: for performance, lock on the hostname instead of global
	s.subsLk.Lock()
	defer s.subsLk.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	sub := Subscription{
		Hostname: host.Hostname,
		HostID:   host.ID,
		ctx:      ctx,
		cancel:   cancel,
	}
	s.subs[host.Hostname] = &sub

	s.GetOrCreateLimiters(host.ID)

	go s.subscribeWithRedialer(ctx, host, &sub, newHost)

	return nil
}

func (s *Slurper) subscribeWithRedialer(ctx context.Context, host *models.Host, sub *Subscription, newHost bool) {
	defer func() {
		s.subsLk.Lock()
		defer s.subsLk.Unlock()

		delete(s.subs, host.Hostname)
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
				if err := s.Config.PersistHostStatusCallback(ctx, sub.HostID, models.HostStatusOffline); err != nil {
					s.logger.Error("failed mark host as stale", "hostname", sub.Hostname, "err", err)
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
			if err := s.processCallback(context.TODO(), &stream.XRPCStreamEvent{RepoCommit: evt}, sub.Hostname, sub.HostID); err != nil {
				logger.Error("failed handling event", "err", err)
			}
			*lastCursor = evt.Seq

			sub.UpdateSeq(*lastCursor)

			return nil
		},
		RepoSync: func(evt *comatproto.SyncSubscribeRepos_Sync) error {
			logger := s.logger.With("host", sub.Hostname, "did", evt.Did, "seq", evt.Seq, "eventType", "sync")
			logger.Debug("got remote repo event")
			if err := s.processCallback(context.TODO(), &stream.XRPCStreamEvent{RepoSync: evt}, sub.Hostname, sub.HostID); err != nil {
				s.logger.Error("failed handling event", "err", err)
			}
			*lastCursor = evt.Seq

			sub.UpdateSeq(*lastCursor)

			return nil
		},
		RepoIdentity: func(evt *comatproto.SyncSubscribeRepos_Identity) error {
			logger := s.logger.With("host", sub.Hostname, "did", evt.Did, "seq", evt.Seq, "eventType", "identity")
			logger.Debug("identity event")
			if err := s.processCallback(context.TODO(), &stream.XRPCStreamEvent{RepoIdentity: evt}, sub.Hostname, sub.HostID); err != nil {
				logger.Error("failed handling event", "err", err)
			}
			*lastCursor = evt.Seq

			sub.UpdateSeq(*lastCursor)

			return nil
		},
		RepoAccount: func(evt *comatproto.SyncSubscribeRepos_Account) error {
			logger := s.logger.With("host", sub.Hostname, "did", evt.Did, "seq", evt.Seq, "eventType", "account")
			s.logger.Debug("account event")
			if err := s.processCallback(context.TODO(), &stream.XRPCStreamEvent{RepoAccount: evt}, sub.Hostname, sub.HostID); err != nil {
				logger.Error("failed handling event", "err", err)
			}
			*lastCursor = evt.Seq

			sub.UpdateSeq(*lastCursor)

			return nil
		},
		Error: func(evt *stream.ErrorFrame) error {
			// TODO: verbose logging
			switch evt.Error {
			case "FutureCursor":
				// TODO: need test coverage for this code path (including re-connect)
				// if we get a FutureCursor frame, reset our sequence number for this host
				if s.Config.PersistCursorCallback != nil {
					hc := []HostCursor{sub.HostCursor()}
					if err := s.Config.PersistCursorCallback(context.Background(), &hc); err != nil {
						s.logger.Error("failed to reset cursor for host which sent FutureCursor error message", "hostname", sub.Hostname, "err", err)
					}
				} else {
					s.logger.Warn("skipping FutureCursor fix because PersistCursorCallback registered", "hostname", sub.Hostname)
				}
				*lastCursor = 0
				// TODO: should this really return an error?
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
			if err := s.processCallback(context.TODO(), &stream.XRPCStreamEvent{RepoHandle: evt}, sub.Hostname, sub.HostID); err != nil {
				logger.Error("failed handling event", "err", err)
			}
			*lastCursor = evt.Seq

			sub.UpdateSeq(*lastCursor)

			return nil
		},
		RepoMigrate: func(evt *comatproto.SyncSubscribeRepos_Migrate) error { // DEPRECATED
			logger := s.logger.With("host", sub.Hostname, "did", evt.Did, "seq", evt.Seq, "eventType", "migrate")
			logger.Debug("got remote repo migrate event", "migrateTo", evt.MigrateTo)
			if err := s.processCallback(context.TODO(), &stream.XRPCStreamEvent{RepoMigrate: evt}, sub.Hostname, sub.HostID); err != nil {
				logger.Error("failed handling event", "err", err)
			}
			*lastCursor = evt.Seq

			sub.UpdateSeq(*lastCursor)

			return nil
		},
		RepoTombstone: func(evt *comatproto.SyncSubscribeRepos_Tombstone) error { // DEPRECATED
			logger := s.logger.With("host", sub.Hostname, "did", evt.Did, "seq", evt.Seq, "eventType", "tombstone")
			logger.Debug("got remote repo tombstone event")
			if err := s.processCallback(context.TODO(), &stream.XRPCStreamEvent{RepoTombstone: evt}, sub.Hostname, sub.HostID); err != nil {
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

type HostCursor struct {
	HostID  uint64
	LastSeq int64
}

// persistCursors sends all cursors to callback to be persisted in database (if registered)
func (s *Slurper) persistCursors(ctx context.Context) error {
	if s.Config.PersistCursorCallback != nil {
		s.logger.Warn("skipping cursor persist because no PersistCursorCallback registered")
		return nil
	}
	start := time.Now()

	// gather cursors: lock overall set, then lock each individual subscription while gathering
	s.subsLk.Lock()
	cursors := make([]HostCursor, len(s.subs))
	i := 0
	for _, sub := range s.subs {
		sub.lk.RLock()
		cursors[i] = HostCursor{
			HostID:  sub.HostID,
			LastSeq: sub.LastSeq,
		}
		sub.lk.RUnlock()
		i++
	}
	s.subsLk.Unlock()

	err := s.Config.PersistCursorCallback(ctx, &cursors)
	s.logger.Info("finished persisting cursors", "count", len(cursors), "duration", time.Since(start), "err", err)
	return err
}

// TODO: called from admin endpoint
func (s *Slurper) GetActiveList() []string {
	s.subsLk.Lock()
	defer s.subsLk.Unlock()
	var out []string
	for k := range s.subs {
		out = append(out, k)
	}

	return out
}

func (s *Slurper) KillUpstreamConnection(hostname string, ban bool) error {
	s.subsLk.Lock()
	defer s.subsLk.Unlock()

	sub, ok := s.subs[hostname]
	if !ok {
		return fmt.Errorf("killing connection %q: %w", hostname, ErrNoActiveConnection)
	}
	sub.cancel()
	// cleanup in the run thread subscribeWithRedialer() will delete(s.active, host)

	if ban && s.Config.PersistHostStatusCallback != nil {
		if err := s.Config.PersistHostStatusCallback(context.TODO(), sub.HostID, models.HostStatusBanned); err != nil {
			return fmt.Errorf("failed to set host as banned: %w", err)
		}
	}

	return nil
}
