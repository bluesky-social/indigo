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

type ProcessMessageFunc func(context.Context, *models.Host, *stream.XRPCStreamEvent) error

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

	log *slog.Logger
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
	Host     *models.Host
	LastSeq  int64 // XXX: switch to an atomic
	Limiters *Limiters

	lk     sync.RWMutex
	ctx    context.Context
	cancel func()
}

func (sub *Subscription) UpdateSeq(seq int64) {
	sub.lk.Lock()
	defer sub.lk.Unlock()
	sub.Host.LastSeq = seq
}

func NewSlurper(db *gorm.DB, cb ProcessMessageFunc, config *SlurperConfig, logger *slog.Logger) (*Slurper, error) {
	if config == nil {
		config = DefaultSlurperConfig()
	}
	if logger == nil {
		logger = slog.Default()
	}

	// NOTE: unused second argument is not an 'error
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
		log:                  logger,
	}

	// Start a goroutine to flush cursors to the DB every 30s
	go func() {
		for {
			select {
			case <-s.shutdownChan:
				s.log.Info("flushing Host cursors on shutdown")
				ctx := context.Background()
				var errs []error
				if errs = s.flushCursors(ctx); len(errs) > 0 {
					for _, err := range errs {
						s.log.Error("failed to flush cursors on shutdown", "err", err)
					}
				}
				s.log.Info("done flushing Host cursors on shutdown")
				s.shutdownResult <- errs
				return
			case <-time.After(time.Second * 10):
				s.log.Debug("flushing Host cursors")
				ctx := context.Background()
				if errs := s.flushCursors(ctx); len(errs) > 0 {
					for _, err := range errs {
						s.log.Error("failed to flush cursors", "err", err)
					}
				}
				s.log.Debug("done flushing Host cursors")
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
	s.log.Info("waiting for slurper shutdown")
	errs := <-s.shutdownResult
	if len(errs) > 0 {
		for _, err := range errs {
			s.log.Error("shutdown error", "err", err)
		}
	}
	s.log.Info("slurper shutdown complete")
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
		Host:   host,
		ctx:    ctx,
		cancel: cancel,
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

	protocol := "ws"
	if s.Config.SSL {
		protocol = "wss"
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

		var url string
		if newHost {
			url = fmt.Sprintf("%s://%s/xrpc/com.atproto.sync.subscribeRepos", protocol, host.Hostname)
		} else {
			url = fmt.Sprintf("%s://%s/xrpc/com.atproto.sync.subscribeRepos?cursor=%d", protocol, host.Hostname, cursor)
		}
		con, res, err := d.DialContext(ctx, url, nil)
		if err != nil {
			s.log.Warn("dialing failed", "host", host.Hostname, "err", err, "backoff", backoff)
			time.Sleep(sleepForBackoff(backoff))
			backoff++

			if backoff > 15 {
				s.log.Warn("host does not appear to be online, disabling for now", "host", host.Hostname)
				if err := s.db.Model(&models.Host{}).Where("id = ?", host.ID).Update("registered", false).Error; err != nil {
					s.log.Error("failed to unregister failing host", "err", err)
				}

				return
			}

			continue
		}

		s.log.Info("event subscription response", "code", res.StatusCode, "url", url)

		curCursor := cursor
		if err := s.handleConnection(ctx, host, con, &cursor, sub); err != nil {
			if errors.Is(err, ErrTimeoutShutdown) {
				s.log.Info("shutting down host subscription after timeout", "host", host.Hostname, "time", EventsTimeout)
				return
			}
			s.log.Warn("connection to failed", "host", host.Hostname, "err", err)
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

func (s *Slurper) handleConnection(ctx context.Context, host *models.Host, con *websocket.Conn, lastCursor *int64, sub *Subscription) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	rsc := &stream.RepoStreamCallbacks{
		RepoCommit: func(evt *comatproto.SyncSubscribeRepos_Commit) error {
			s.log.Debug("got remote repo event", "host", host.Hostname, "repo", evt.Repo, "seq", evt.Seq)
			if err := s.cb(context.TODO(), host, &stream.XRPCStreamEvent{
				RepoCommit: evt,
			}); err != nil {
				s.log.Error("failed handling event", "host", host.Hostname, "seq", evt.Seq, "err", err)
			}
			*lastCursor = evt.Seq

			sub.UpdateSeq(*lastCursor)

			return nil
		},
		RepoSync: func(evt *comatproto.SyncSubscribeRepos_Sync) error {
			s.log.Debug("got remote repo event", "host", host.Hostname, "repo", evt.Did, "seq", evt.Seq)
			if err := s.cb(context.TODO(), host, &stream.XRPCStreamEvent{
				RepoSync: evt,
			}); err != nil {
				s.log.Error("failed handling event", "host", host.Hostname, "seq", evt.Seq, "err", err)
			}
			*lastCursor = evt.Seq

			sub.UpdateSeq(*lastCursor)

			return nil
		},
		RepoHandle: func(evt *comatproto.SyncSubscribeRepos_Handle) error {
			s.log.Debug("got remote handle update event", "host", host.Hostname, "did", evt.Did, "handle", evt.Handle)
			if err := s.cb(context.TODO(), host, &stream.XRPCStreamEvent{
				RepoHandle: evt,
			}); err != nil {
				s.log.Error("failed handling event", "host", host.Hostname, "seq", evt.Seq, "err", err)
			}
			*lastCursor = evt.Seq

			sub.UpdateSeq(*lastCursor)

			return nil
		},
		RepoMigrate: func(evt *comatproto.SyncSubscribeRepos_Migrate) error {
			s.log.Debug("got remote repo migrate event", "host", host.Hostname, "did", evt.Did, "migrateTo", evt.MigrateTo)
			if err := s.cb(context.TODO(), host, &stream.XRPCStreamEvent{
				RepoMigrate: evt,
			}); err != nil {
				s.log.Error("failed handling event", "host", host.Hostname, "seq", evt.Seq, "err", err)
			}
			*lastCursor = evt.Seq

			sub.UpdateSeq(*lastCursor)

			return nil
		},
		RepoTombstone: func(evt *comatproto.SyncSubscribeRepos_Tombstone) error {
			s.log.Debug("got remote repo tombstone event", "host", host.Hostname, "did", evt.Did)
			if err := s.cb(context.TODO(), host, &stream.XRPCStreamEvent{
				RepoTombstone: evt,
			}); err != nil {
				s.log.Error("failed handling event", "host", host.Hostname, "seq", evt.Seq, "err", err)
			}
			*lastCursor = evt.Seq

			sub.UpdateSeq(*lastCursor)

			return nil
		},
		RepoInfo: func(info *comatproto.SyncSubscribeRepos_Info) error {
			s.log.Debug("info event", "name", info.Name, "message", info.Message, "host", host.Hostname)
			return nil
		},
		RepoIdentity: func(ident *comatproto.SyncSubscribeRepos_Identity) error {
			s.log.Debug("identity event", "did", ident.Did)
			if err := s.cb(context.TODO(), host, &stream.XRPCStreamEvent{
				RepoIdentity: ident,
			}); err != nil {
				s.log.Error("failed handling event", "host", host.Hostname, "seq", ident.Seq, "err", err)
			}
			*lastCursor = ident.Seq

			sub.UpdateSeq(*lastCursor)

			return nil
		},
		RepoAccount: func(acct *comatproto.SyncSubscribeRepos_Account) error {
			s.log.Debug("account event", "did", acct.Did, "status", acct.Status)
			if err := s.cb(context.TODO(), host, &stream.XRPCStreamEvent{
				RepoAccount: acct,
			}); err != nil {
				s.log.Error("failed handling event", "host", host.Hostname, "seq", acct.Seq, "err", err)
			}
			*lastCursor = acct.Seq

			sub.UpdateSeq(*lastCursor)

			return nil
		},
		// TODO: all the other event types (handle change, migration, etc)
		Error: func(errf *stream.ErrorFrame) error {
			switch errf.Error {
			case "FutureCursor":
				// XXX: need test coverage for this path
				// if we get a FutureCursor frame, reset our sequence number for this host
				if err := s.db.Table("host").Where("id = ?", host.ID).Update("last_seq", 0).Error; err != nil {
					return err
				}

				*lastCursor = 0
				return fmt.Errorf("got FutureCursor frame, reset cursor tracking for host")
			default:
				return fmt.Errorf("error frame: %s: %s", errf.Error, errf.Message)
			}
		},
	}

	lims := s.GetOrCreateLimiters(host.ID)

	limiters := []*slidingwindow.Limiter{
		lims.PerSecond,
		lims.PerHour,
		lims.PerDay,
	}

	instrumentedRSC := stream.NewInstrumentedRepoStreamCallbacks(limiters, rsc.EventHandler)

	pool := parallel.NewScheduler(
		100,
		1_000,
		con.RemoteAddr().String(),
		instrumentedRSC.EventHandler,
	)
	return stream.HandleRepoStream(ctx, con, pool, nil)
}

type cursorSnapshot struct {
	id     uint64
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
			id:     sub.Host.ID,
			cursor: sub.Host.LastSeq,
		})
		sub.lk.RUnlock()
	}
	s.lk.Unlock()

	errs := []error{}
	okcount := 0

	tx := s.db.WithContext(ctx).Begin()
	for _, cursor := range cursors {
		if err := tx.WithContext(ctx).Model(models.Host{}).Where("id = ?", cursor.id).UpdateColumn("last_seq", cursor.cursor).Error; err != nil {
			errs = append(errs, err)
		} else {
			okcount++
		}
	}
	if err := tx.WithContext(ctx).Commit().Error; err != nil {
		errs = append(errs, err)
	}
	dt := time.Since(start)
	s.log.Info("flushCursors", "dt", dt, "ok", okcount, "errs", len(errs))

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

func (s *Slurper) KillUpstreamConnection(host string, block bool) error {
	s.lk.Lock()
	defer s.lk.Unlock()

	ac, ok := s.active[host]
	if !ok {
		return fmt.Errorf("killing connection %q: %w", host, ErrNoActiveConnection)
	}
	ac.cancel()
	// cleanup in the run thread subscribeWithRedialer() will delete(s.active, host)

	if block {
		if err := s.db.Model(models.Host{}).Where("id = ?", ac.Host.ID).UpdateColumn("blocked", true).Error; err != nil {
			return fmt.Errorf("failed to set host as blocked: %w", err)
		}
	}

	return nil
}
