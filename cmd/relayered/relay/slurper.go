package relay

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/RussellLuo/slidingwindow"
	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/cmd/relayered/relay/models"
	"github.com/bluesky-social/indigo/cmd/relayered/stream"
	"github.com/bluesky-social/indigo/cmd/relayered/stream/schedulers/parallel"

	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

type IndexCallback func(context.Context, *models.PDS, *stream.XRPCStreamEvent) error

type Slurper struct {
	cb     IndexCallback
	db     *gorm.DB
	Config *SlurperConfig

	lk     sync.Mutex
	active map[string]*activeSub

	LimitMux sync.RWMutex
	Limiters map[uint]*Limiters

	DefaultRepoLimit  int64
	ConcurrencyPerPDS int64
	MaxQueuePerPDS    int64

	NewPDSPerDayLimiter *slidingwindow.Limiter

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
	ConcurrencyPerPDS     int64
	MaxQueuePerPDS        int64
	NewSubsDisabled       bool
	TrustedDomains        []string
	NewPDSPerDayLimit     int64
}

func DefaultSlurperConfig() *SlurperConfig {
	return &SlurperConfig{
		SSL:                   false,
		DefaultPerSecondLimit: 50,
		DefaultPerHourLimit:   2500,
		DefaultPerDayLimit:    20_000,
		DefaultRepoLimit:      100,
		ConcurrencyPerPDS:     100,
		MaxQueuePerPDS:        1_000,
	}
}

type activeSub struct {
	pds    *models.PDS
	lk     sync.RWMutex
	ctx    context.Context
	cancel func()
}

func (sub *activeSub) updateCursor(curs int64) {
	sub.lk.Lock()
	defer sub.lk.Unlock()
	sub.pds.Cursor = curs
}

func NewSlurper(db *gorm.DB, cb IndexCallback, config *SlurperConfig, logger *slog.Logger) (*Slurper, error) {
	if config == nil {
		config = DefaultSlurperConfig()
	}
	if logger == nil {
		logger = slog.Default()
	}

	// NOTE: unused second argument is not an 'error
	newPDSPerDayLimiter, _ := slidingwindow.NewLimiter(time.Hour*24, config.NewPDSPerDayLimit, windowFunc)

	s := &Slurper{
		cb:                  cb,
		db:                  db,
		Config:              config,
		active:              make(map[string]*activeSub),
		Limiters:            make(map[uint]*Limiters),
		shutdownChan:        make(chan bool),
		shutdownResult:      make(chan []error),
		NewPDSPerDayLimiter: newPDSPerDayLimiter,
		log:                 logger,
	}

	// Start a goroutine to flush cursors to the DB every 30s
	go func() {
		for {
			select {
			case <-s.shutdownChan:
				s.log.Info("flushing PDS cursors on shutdown")
				ctx := context.Background()
				var errs []error
				if errs = s.flushCursors(ctx); len(errs) > 0 {
					for _, err := range errs {
						s.log.Error("failed to flush cursors on shutdown", "err", err)
					}
				}
				s.log.Info("done flushing PDS cursors on shutdown")
				s.shutdownResult <- errs
				return
			case <-time.After(time.Second * 10):
				s.log.Debug("flushing PDS cursors")
				ctx := context.Background()
				if errs := s.flushCursors(ctx); len(errs) > 0 {
					for _, err := range errs {
						s.log.Error("failed to flush cursors", "err", err)
					}
				}
				s.log.Debug("done flushing PDS cursors")
			}
		}
	}()

	return s, nil
}

func windowFunc() (slidingwindow.Window, slidingwindow.StopFunc) {
	return slidingwindow.NewLocalWindow()
}

func (s *Slurper) GetLimiters(pdsID uint) *Limiters {
	s.LimitMux.RLock()
	defer s.LimitMux.RUnlock()
	return s.Limiters[pdsID]
}

func (s *Slurper) GetOrCreateLimiters(pdsID uint, perSecLimit int64, perHourLimit int64, perDayLimit int64) *Limiters {
	s.LimitMux.RLock()
	defer s.LimitMux.RUnlock()
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

func (s *Slurper) SetLimits(pdsID uint, perSecLimit int64, perHourLimit int64, perDayLimit int64) {
	s.LimitMux.Lock()
	defer s.LimitMux.Unlock()
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

var ErrNewSubsDisabled = fmt.Errorf("new subscriptions temporarily disabled")

// Checks whether a host is allowed to be subscribed to
// must be called with the slurper lock held
func (s *Slurper) canSlurpHost(host string) bool {
	// Check if we're over the limit for new PDSs today
	if !s.NewPDSPerDayLimiter.Allow() {
		return false
	}

	// Check if the host is a trusted domain
	for _, d := range s.Config.TrustedDomains {
		// If the domain starts with a *., it's a wildcard
		if strings.HasPrefix(d, "*.") {
			// Cut off the * so we have .domain.com
			if strings.HasSuffix(host, strings.TrimPrefix(d, "*")) {
				return true
			}
		} else {
			if host == d {
				return true
			}
		}
	}

	return !s.Config.NewSubsDisabled
}

func (s *Slurper) SubscribeToPds(ctx context.Context, host string, reg bool, adminOverride bool, rateOverrides *PDSRates) error {
	// TODO: for performance, lock on the hostname instead of global
	s.lk.Lock()
	defer s.lk.Unlock()

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

	newHost := false

	if peering.ID == 0 {
		if !adminOverride && !s.canSlurpHost(host) {
			return ErrNewSubsDisabled
		}
		// New PDS!
		npds := models.PDS{
			Host:             host,
			SSL:              s.Config.SSL,
			Registered:       reg,
			RateLimit:        float64(s.Config.DefaultPerSecondLimit),
			HourlyEventLimit: s.Config.DefaultPerHourLimit,
			DailyEventLimit:  s.Config.DefaultPerDayLimit,
			RepoLimit:        s.Config.DefaultRepoLimit,
		}
		if rateOverrides != nil {
			npds.RateLimit = float64(rateOverrides.PerSecond)
			npds.HourlyEventLimit = rateOverrides.PerHour
			npds.DailyEventLimit = rateOverrides.PerDay
			npds.RepoLimit = rateOverrides.RepoLimit
		}
		if err := s.db.Create(&npds).Error; err != nil {
			return err
		}

		newHost = true
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

	s.GetOrCreateLimiters(peering.ID, int64(peering.RateLimit), peering.HourlyEventLimit, peering.DailyEventLimit)

	go s.subscribeWithRedialer(ctx, &peering, &sub, newHost)

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

		// Check if we've already got a limiter for this PDS
		s.GetOrCreateLimiters(pds.ID, int64(pds.RateLimit), pds.HourlyEventLimit, pds.DailyEventLimit)
		go s.subscribeWithRedialer(ctx, &pds, &sub, false)
	}

	return nil
}

func (s *Slurper) subscribeWithRedialer(ctx context.Context, host *models.PDS, sub *activeSub, newHost bool) {
	defer func() {
		s.lk.Lock()
		defer s.lk.Unlock()

		delete(s.active, host.Host)
	}()

	d := websocket.Dialer{
		HandshakeTimeout: time.Second * 5,
	}

	protocol := "ws"
	if s.Config.SSL {
		protocol = "wss"
	}

	// Special case `.host.bsky.network` PDSs to rewind cursor by 200 events to smooth over unclean shutdowns
	if strings.HasSuffix(host.Host, ".host.bsky.network") && host.Cursor > 200 {
		host.Cursor -= 200
	}

	cursor := host.Cursor

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
			url = fmt.Sprintf("%s://%s/xrpc/com.atproto.sync.subscribeRepos", protocol, host.Host)
		} else {
			url = fmt.Sprintf("%s://%s/xrpc/com.atproto.sync.subscribeRepos?cursor=%d", protocol, host.Host, cursor)
		}
		con, res, err := d.DialContext(ctx, url, nil)
		if err != nil {
			s.log.Warn("dialing failed", "pdsHost", host.Host, "err", err, "backoff", backoff)
			time.Sleep(sleepForBackoff(backoff))
			backoff++

			if backoff > 15 {
				s.log.Warn("pds does not appear to be online, disabling for now", "pdsHost", host.Host)
				if err := s.db.Model(&models.PDS{}).Where("id = ?", host.ID).Update("registered", false).Error; err != nil {
					s.log.Error("failed to unregister failing pds", "err", err)
				}

				return
			}

			continue
		}

		s.log.Info("event subscription response", "code", res.StatusCode, "url", url)

		curCursor := cursor
		if err := s.handleConnection(ctx, host, con, &cursor, sub); err != nil {
			if errors.Is(err, ErrTimeoutShutdown) {
				s.log.Info("shutting down pds subscription after timeout", "host", host.Host, "time", EventsTimeout)
				return
			}
			s.log.Warn("connection to failed", "host", host.Host, "err", err)
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

var ErrTimeoutShutdown = fmt.Errorf("timed out waiting for new events")

var EventsTimeout = time.Minute

func (s *Slurper) handleConnection(ctx context.Context, host *models.PDS, con *websocket.Conn, lastCursor *int64, sub *activeSub) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	rsc := &stream.RepoStreamCallbacks{
		RepoCommit: func(evt *comatproto.SyncSubscribeRepos_Commit) error {
			s.log.Debug("got remote repo event", "pdsHost", host.Host, "repo", evt.Repo, "seq", evt.Seq)
			if err := s.cb(context.TODO(), host, &stream.XRPCStreamEvent{
				RepoCommit: evt,
			}); err != nil {
				s.log.Error("failed handling event", "host", host.Host, "seq", evt.Seq, "err", err)
			}
			*lastCursor = evt.Seq

			sub.updateCursor(*lastCursor)

			return nil
		},
		RepoSync: func(evt *comatproto.SyncSubscribeRepos_Sync) error {
			s.log.Debug("got remote repo event", "pdsHost", host.Host, "repo", evt.Did, "seq", evt.Seq)
			if err := s.cb(context.TODO(), host, &stream.XRPCStreamEvent{
				RepoSync: evt,
			}); err != nil {
				s.log.Error("failed handling event", "host", host.Host, "seq", evt.Seq, "err", err)
			}
			*lastCursor = evt.Seq

			sub.updateCursor(*lastCursor)

			return nil
		},
		RepoHandle: func(evt *comatproto.SyncSubscribeRepos_Handle) error {
			s.log.Debug("got remote handle update event", "pdsHost", host.Host, "did", evt.Did, "handle", evt.Handle)
			if err := s.cb(context.TODO(), host, &stream.XRPCStreamEvent{
				RepoHandle: evt,
			}); err != nil {
				s.log.Error("failed handling event", "host", host.Host, "seq", evt.Seq, "err", err)
			}
			*lastCursor = evt.Seq

			sub.updateCursor(*lastCursor)

			return nil
		},
		RepoMigrate: func(evt *comatproto.SyncSubscribeRepos_Migrate) error {
			s.log.Debug("got remote repo migrate event", "pdsHost", host.Host, "did", evt.Did, "migrateTo", evt.MigrateTo)
			if err := s.cb(context.TODO(), host, &stream.XRPCStreamEvent{
				RepoMigrate: evt,
			}); err != nil {
				s.log.Error("failed handling event", "host", host.Host, "seq", evt.Seq, "err", err)
			}
			*lastCursor = evt.Seq

			sub.updateCursor(*lastCursor)

			return nil
		},
		RepoTombstone: func(evt *comatproto.SyncSubscribeRepos_Tombstone) error {
			s.log.Debug("got remote repo tombstone event", "pdsHost", host.Host, "did", evt.Did)
			if err := s.cb(context.TODO(), host, &stream.XRPCStreamEvent{
				RepoTombstone: evt,
			}); err != nil {
				s.log.Error("failed handling event", "host", host.Host, "seq", evt.Seq, "err", err)
			}
			*lastCursor = evt.Seq

			sub.updateCursor(*lastCursor)

			return nil
		},
		RepoInfo: func(info *comatproto.SyncSubscribeRepos_Info) error {
			s.log.Debug("info event", "name", info.Name, "message", info.Message, "pdsHost", host.Host)
			return nil
		},
		RepoIdentity: func(ident *comatproto.SyncSubscribeRepos_Identity) error {
			s.log.Debug("identity event", "did", ident.Did)
			if err := s.cb(context.TODO(), host, &stream.XRPCStreamEvent{
				RepoIdentity: ident,
			}); err != nil {
				s.log.Error("failed handling event", "host", host.Host, "seq", ident.Seq, "err", err)
			}
			*lastCursor = ident.Seq

			sub.updateCursor(*lastCursor)

			return nil
		},
		RepoAccount: func(acct *comatproto.SyncSubscribeRepos_Account) error {
			s.log.Debug("account event", "did", acct.Did, "status", acct.Status)
			if err := s.cb(context.TODO(), host, &stream.XRPCStreamEvent{
				RepoAccount: acct,
			}); err != nil {
				s.log.Error("failed handling event", "host", host.Host, "seq", acct.Seq, "err", err)
			}
			*lastCursor = acct.Seq

			sub.updateCursor(*lastCursor)

			return nil
		},
		// TODO: all the other event types (handle change, migration, etc)
		Error: func(errf *stream.ErrorFrame) error {
			switch errf.Error {
			case "FutureCursor":
				// if we get a FutureCursor frame, reset our sequence number for this host
				if err := s.db.Table("pds").Where("id = ?", host.ID).Update("cursor", 0).Error; err != nil {
					return err
				}

				*lastCursor = 0
				return fmt.Errorf("got FutureCursor frame, reset cursor tracking for host")
			default:
				return fmt.Errorf("error frame: %s: %s", errf.Error, errf.Message)
			}
		},
	}

	lims := s.GetOrCreateLimiters(host.ID, int64(host.RateLimit), host.HourlyEventLimit, host.DailyEventLimit)

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
	id     uint
	cursor int64
}

// flushCursors updates the PDS cursors in the DB for all active subscriptions
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
			id:     sub.pds.ID,
			cursor: sub.pds.Cursor,
		})
		sub.lk.RUnlock()
	}
	s.lk.Unlock()

	errs := []error{}
	okcount := 0

	tx := s.db.WithContext(ctx).Begin()
	for _, cursor := range cursors {
		if err := tx.WithContext(ctx).Model(models.PDS{}).Where("id = ?", cursor.id).UpdateColumn("cursor", cursor.cursor).Error; err != nil {
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

var ErrNoActiveConnection = fmt.Errorf("no active connection to host")

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
		if err := s.db.Model(models.PDS{}).Where("id = ?", ac.pds.ID).UpdateColumn("blocked", true).Error; err != nil {
			return fmt.Errorf("failed to set host as blocked: %w", err)
		}
	}

	return nil
}
