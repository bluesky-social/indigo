package bgs

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
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/events/schedulers/parallel"
	"github.com/bluesky-social/indigo/models"
	"go.opentelemetry.io/otel"
	"golang.org/x/time/rate"

	"github.com/gorilla/websocket"
	pq "github.com/lib/pq"
	"gorm.io/gorm"
)

var log = slog.Default().With("system", "bgs")

type IndexCallback func(context.Context, *models.PDS, *events.XRPCStreamEvent) error

// TODO: rename me
type Slurper struct {
	cb IndexCallback

	db *gorm.DB

	lk     sync.Mutex
	active map[string]*activeSub

	LimitMux              sync.RWMutex
	Limiters              map[uint]*Limiters
	DefaultPerSecondLimit int64
	DefaultPerHourLimit   int64
	DefaultPerDayLimit    int64

	DefaultCrawlLimit rate.Limit
	DefaultRepoLimit  int64
	ConcurrencyPerPDS int64
	MaxQueuePerPDS    int64

	NewPDSPerDayLimiter *slidingwindow.Limiter

	newSubsDisabled bool
	trustedDomains  []string

	shutdownChan   chan bool
	shutdownResult chan []error

	ssl bool
}

type Limiters struct {
	PerSecond *slidingwindow.Limiter
	PerHour   *slidingwindow.Limiter
	PerDay    *slidingwindow.Limiter
}

type SlurperOptions struct {
	SSL                   bool
	DefaultPerSecondLimit int64
	DefaultPerHourLimit   int64
	DefaultPerDayLimit    int64
	DefaultCrawlLimit     rate.Limit
	DefaultRepoLimit      int64
	ConcurrencyPerPDS     int64
	MaxQueuePerPDS        int64
}

func DefaultSlurperOptions() *SlurperOptions {
	return &SlurperOptions{
		SSL:                   false,
		DefaultPerSecondLimit: 50,
		DefaultPerHourLimit:   2500,
		DefaultPerDayLimit:    20_000,
		DefaultCrawlLimit:     rate.Limit(5),
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

func NewSlurper(db *gorm.DB, cb IndexCallback, opts *SlurperOptions) (*Slurper, error) {
	if opts == nil {
		opts = DefaultSlurperOptions()
	}
	db.AutoMigrate(&SlurpConfig{})
	s := &Slurper{
		cb:                    cb,
		db:                    db,
		active:                make(map[string]*activeSub),
		Limiters:              make(map[uint]*Limiters),
		DefaultPerSecondLimit: opts.DefaultPerSecondLimit,
		DefaultPerHourLimit:   opts.DefaultPerHourLimit,
		DefaultPerDayLimit:    opts.DefaultPerDayLimit,
		DefaultCrawlLimit:     opts.DefaultCrawlLimit,
		DefaultRepoLimit:      opts.DefaultRepoLimit,
		ConcurrencyPerPDS:     opts.ConcurrencyPerPDS,
		MaxQueuePerPDS:        opts.MaxQueuePerPDS,
		ssl:                   opts.SSL,
		shutdownChan:          make(chan bool),
		shutdownResult:        make(chan []error),
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
						log.Error("failed to flush cursors on shutdown", "err", err)
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
						log.Error("failed to flush cursors", "err", err)
					}
				}
				log.Debug("done flushing PDS cursors")
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
	log.Info("waiting for slurper shutdown")
	errs := <-s.shutdownResult
	if len(errs) > 0 {
		for _, err := range errs {
			log.Error("shutdown error", "err", err)
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
	s.trustedDomains = sc.TrustedDomains

	s.NewPDSPerDayLimiter, _ = slidingwindow.NewLimiter(time.Hour*24, sc.NewPDSPerDayLimit, windowFunc)

	return nil
}

type SlurpConfig struct {
	gorm.Model

	NewSubsDisabled   bool
	TrustedDomains    pq.StringArray `gorm:"type:text[]"`
	NewPDSPerDayLimit int64
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

func (s *Slurper) SetNewPDSPerDayLimit(limit int64) error {
	s.lk.Lock()
	defer s.lk.Unlock()

	if err := s.db.Model(SlurpConfig{}).Where("id = 1").Update("new_pds_per_day_limit", limit).Error; err != nil {
		return err
	}

	s.NewPDSPerDayLimiter.SetLimit(limit)
	return nil
}

func (s *Slurper) GetNewPDSPerDayLimit() int64 {
	s.lk.Lock()
	defer s.lk.Unlock()
	return s.NewPDSPerDayLimiter.Limit()
}

func (s *Slurper) AddTrustedDomain(domain string) error {
	s.lk.Lock()
	defer s.lk.Unlock()

	if err := s.db.Model(SlurpConfig{}).Where("id = 1").Update("trusted_domains", gorm.Expr("array_append(trusted_domains, ?)", domain)).Error; err != nil {
		return err
	}

	s.trustedDomains = append(s.trustedDomains, domain)
	return nil
}

func (s *Slurper) RemoveTrustedDomain(domain string) error {
	s.lk.Lock()
	defer s.lk.Unlock()

	if err := s.db.Model(SlurpConfig{}).Where("id = 1").Update("trusted_domains", gorm.Expr("array_remove(trusted_domains, ?)", domain)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}

	for i, d := range s.trustedDomains {
		if d == domain {
			s.trustedDomains = append(s.trustedDomains[:i], s.trustedDomains[i+1:]...)
			break
		}
	}

	return nil
}

func (s *Slurper) SetTrustedDomains(domains []string) error {
	s.lk.Lock()
	defer s.lk.Unlock()

	if err := s.db.Model(SlurpConfig{}).Where("id = 1").Update("trusted_domains", domains).Error; err != nil {
		return err
	}

	s.trustedDomains = domains
	return nil
}

func (s *Slurper) GetTrustedDomains() []string {
	s.lk.Lock()
	defer s.lk.Unlock()
	return s.trustedDomains
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
	for _, d := range s.trustedDomains {
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

	return !s.newSubsDisabled
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

	if peering.ID == 0 {
		if !adminOverride && !s.canSlurpHost(host) {
			return ErrNewSubsDisabled
		}
		// New PDS!
		npds := models.PDS{
			Host:             host,
			SSL:              s.ssl,
			Registered:       reg,
			RateLimit:        float64(s.DefaultPerSecondLimit),
			HourlyEventLimit: s.DefaultPerHourLimit,
			DailyEventLimit:  s.DefaultPerDayLimit,
			CrawlRateLimit:   float64(s.DefaultCrawlLimit),
			RepoLimit:        s.DefaultRepoLimit,
		}
		if rateOverrides != nil {
			npds.RateLimit = float64(rateOverrides.PerSecond)
			npds.HourlyEventLimit = rateOverrides.PerHour
			npds.DailyEventLimit = rateOverrides.PerDay
			npds.CrawlRateLimit = float64(rateOverrides.CrawlRate)
			npds.RepoLimit = rateOverrides.RepoLimit
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

	s.GetOrCreateLimiters(peering.ID, int64(peering.RateLimit), peering.HourlyEventLimit, peering.DailyEventLimit)

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

		// Check if we've already got a limiter for this PDS
		s.GetOrCreateLimiters(pds.ID, int64(pds.RateLimit), pds.HourlyEventLimit, pds.DailyEventLimit)
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

	d := websocket.Dialer{
		HandshakeTimeout: time.Second * 5,
	}

	protocol := "ws"
	if s.ssl {
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

		url := fmt.Sprintf("%s://%s/xrpc/com.atproto.sync.subscribeRepos?cursor=%d", protocol, host.Host, cursor)
		con, res, err := d.DialContext(ctx, url, nil)
		if err != nil {
			log.Warn("dialing failed", "pdsHost", host.Host, "err", err, "backoff", backoff)
			time.Sleep(sleepForBackoff(backoff))
			backoff++

			if backoff > 15 {
				log.Warn("pds does not appear to be online, disabling for now", "pdsHost", host.Host)
				if err := s.db.Model(&models.PDS{}).Where("id = ?", host.ID).Update("registered", false).Error; err != nil {
					log.Error("failed to unregister failing pds", "err", err)
				}

				return
			}

			continue
		}

		log.Info("event subscription response", "code", res.StatusCode)

		curCursor := cursor
		if err := s.handleConnection(ctx, host, con, &cursor, sub); err != nil {
			if errors.Is(err, ErrTimeoutShutdown) {
				log.Info("shutting down pds subscription after timeout", "host", host.Host, "time", EventsTimeout)
				return
			}
			log.Warn("connection to failed", "host", host.Host, "err", err)
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

	rsc := &events.RepoStreamCallbacks{
		RepoCommit: func(evt *comatproto.SyncSubscribeRepos_Commit) error {
			log.Debug("got remote repo event", "pdsHost", host.Host, "repo", evt.Repo, "seq", evt.Seq)
			if err := s.cb(context.TODO(), host, &events.XRPCStreamEvent{
				RepoCommit: evt,
			}); err != nil {
				log.Error("failed handling event", "host", host.Host, "seq", evt.Seq, "err", err)
			}
			*lastCursor = evt.Seq

			if err := s.updateCursor(sub, *lastCursor); err != nil {
				return fmt.Errorf("updating cursor: %w", err)
			}

			return nil
		},
		RepoHandle: func(evt *comatproto.SyncSubscribeRepos_Handle) error {
			log.Info("got remote handle update event", "pdsHost", host.Host, "did", evt.Did, "handle", evt.Handle)
			if err := s.cb(context.TODO(), host, &events.XRPCStreamEvent{
				RepoHandle: evt,
			}); err != nil {
				log.Error("failed handling event", "host", host.Host, "seq", evt.Seq, "err", err)
			}
			*lastCursor = evt.Seq

			if err := s.updateCursor(sub, *lastCursor); err != nil {
				return fmt.Errorf("updating cursor: %w", err)
			}

			return nil
		},
		RepoMigrate: func(evt *comatproto.SyncSubscribeRepos_Migrate) error {
			log.Info("got remote repo migrate event", "pdsHost", host.Host, "did", evt.Did, "migrateTo", evt.MigrateTo)
			if err := s.cb(context.TODO(), host, &events.XRPCStreamEvent{
				RepoMigrate: evt,
			}); err != nil {
				log.Error("failed handling event", "host", host.Host, "seq", evt.Seq, "err", err)
			}
			*lastCursor = evt.Seq

			if err := s.updateCursor(sub, *lastCursor); err != nil {
				return fmt.Errorf("updating cursor: %w", err)
			}

			return nil
		},
		RepoTombstone: func(evt *comatproto.SyncSubscribeRepos_Tombstone) error {
			log.Info("got remote repo tombstone event", "pdsHost", host.Host, "did", evt.Did)
			if err := s.cb(context.TODO(), host, &events.XRPCStreamEvent{
				RepoTombstone: evt,
			}); err != nil {
				log.Error("failed handling event", "host", host.Host, "seq", evt.Seq, "err", err)
			}
			*lastCursor = evt.Seq

			if err := s.updateCursor(sub, *lastCursor); err != nil {
				return fmt.Errorf("updating cursor: %w", err)
			}

			return nil
		},
		RepoInfo: func(info *comatproto.SyncSubscribeRepos_Info) error {
			log.Info("info event", "name", info.Name, "message", info.Message, "pdsHost", host.Host)
			return nil
		},
		RepoIdentity: func(ident *comatproto.SyncSubscribeRepos_Identity) error {
			log.Info("identity event", "did", ident.Did)
			if err := s.cb(context.TODO(), host, &events.XRPCStreamEvent{
				RepoIdentity: ident,
			}); err != nil {
				log.Error("failed handling event", "host", host.Host, "seq", ident.Seq, "err", err)
			}
			*lastCursor = ident.Seq

			if err := s.updateCursor(sub, *lastCursor); err != nil {
				return fmt.Errorf("updating cursor: %w", err)
			}

			return nil
		},
		RepoAccount: func(acct *comatproto.SyncSubscribeRepos_Account) error {
			log.Info("account event", "did", acct.Did, "status", acct.Status)
			if err := s.cb(context.TODO(), host, &events.XRPCStreamEvent{
				RepoAccount: acct,
			}); err != nil {
				log.Error("failed handling event", "host", host.Host, "seq", acct.Seq, "err", err)
			}
			*lastCursor = acct.Seq

			if err := s.updateCursor(sub, *lastCursor); err != nil {
				return fmt.Errorf("updating cursor: %w", err)
			}

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

	instrumentedRSC := events.NewInstrumentedRepoStreamCallbacks(limiters, rsc.EventHandler)

	pool := parallel.NewScheduler(
		100,
		1_000,
		con.RemoteAddr().String(),
		instrumentedRSC.EventHandler,
	)
	return events.HandleRepoStream(ctx, con, pool, nil)
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
	// cleanup in the run thread subscribeWithRedialer() will delete(s.active, host)

	if block {
		if err := s.db.Model(models.PDS{}).Where("id = ?", ac.pds.ID).UpdateColumn("blocked", true).Error; err != nil {
			return fmt.Errorf("failed to set host as blocked: %w", err)
		}
	}

	return nil
}
