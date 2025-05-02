package relay

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/cmd/relay/relay/models"
	"github.com/bluesky-social/indigo/cmd/relay/stream"
	"github.com/bluesky-social/indigo/cmd/relay/stream/schedulers/parallel"
	"github.com/bluesky-social/indigo/util/ssrf"

	"github.com/RussellLuo/slidingwindow"
	"github.com/gorilla/websocket"
)

var ErrFutureCursor = errors.New("host rejected future cursor")

type ProcessMessageFunc func(ctx context.Context, evt *stream.XRPCStreamEvent, hostname string, hostID uint64) error
type PersistCursorFunc func(ctx context.Context, cursors *[]HostCursor) error
type PersistHostStatusFunc func(ctx context.Context, hostID uint64, state models.HostStatus) error

// `Slurper` is the sub-system of the relay which manages active websocket firehose connections to upstream hosts (eg, PDS instances).
//
// It configures rate-limits, tracks cursors, and retries connections. It passes received messages on to the main relay via a callback function. `Slurper` does not talk to the database directly, but does have some callback to persist host state (cursors and hosting status for some error conditions).
type Slurper struct {
	processCallback ProcessMessageFunc
	Config          *SlurperConfig

	subsLk sync.Mutex
	subs   map[string]*Subscription

	shutdownChan   chan bool
	shutdownResult chan error

	logger *slog.Logger
}

type SlurperConfig struct {
	UserAgent           string
	ConcurrencyPerHost  int
	QueueDepthPerHost   int
	PersistCursorPeriod time.Duration

	BaselinePerSecondLimit int64
	BaselinePerHourLimit   int64
	BaselinePerDayLimit    int64
	TrustedPerSecondLimit  int64
	TrustedPerHourLimit    int64
	TrustedPerDayLimit     int64

	// callback functions. technically optional but effectively required
	PersistCursorCallback     PersistCursorFunc
	PersistHostStatusCallback PersistHostStatusFunc
}

func DefaultSlurperConfig() *SlurperConfig {
	// NOTE: many of these defaults are overruled by DefaultRelayConfig, or even process CLI arg defaults
	return &SlurperConfig{
		UserAgent:          "indigo-relay (atproto-relay)",
		ConcurrencyPerHost: 40,
		// NOTE: queue depth doesn't do anything with current parallel scheduler implementation
		QueueDepthPerHost:   1000,
		PersistCursorPeriod: time.Second * 4,

		// these are the minimum event rates for regular public hosts
		BaselinePerSecondLimit: 50,
		BaselinePerHourLimit:   2500,
		BaselinePerDayLimit:    20_000,

		// these are the fixed event rates for trusted hosts (eg, same service provider as relay)
		TrustedPerSecondLimit: 5_000,
		TrustedPerHourLimit:   50_000_000,
		TrustedPerDayLimit:    500_000_000,
	}
}

// represents an active client connection to a remote host
type Subscription struct {
	Hostname string
	HostID   uint64
	LastSeq  atomic.Int64
	Limiters *StreamLimiters

	scheduler *parallel.Scheduler
	lk        sync.RWMutex
	ctx       context.Context
	cancel    func()
}

// pulls lastSeq from underlying scheduler in to this Subscription
func (sub *Subscription) UpdateSeq() {
	// possible for this to get called before a connection has fully been set up
	if sub.scheduler == nil {
		return
	}
	seq := sub.scheduler.LastSeq()
	if seq > 0 {
		sub.LastSeq.Store(seq)
	}
}

func (sub *Subscription) HostCursor() HostCursor {
	return HostCursor{
		HostID:  sub.HostID,
		LastSeq: sub.LastSeq.Load(),
	}
}

type StreamLimiterCounts struct {
	PerSecond int64
	PerHour   int64
	PerDay    int64
}

type StreamLimiters struct {
	PerSecond *slidingwindow.Limiter
	PerHour   *slidingwindow.Limiter
	PerDay    *slidingwindow.Limiter
}

func (sl *StreamLimiters) Counts() StreamLimiterCounts {
	return StreamLimiterCounts{
		PerSecond: sl.PerSecond.Limit(),
		PerHour:   sl.PerHour.Limit(),
		PerDay:    sl.PerDay.Limit(),
	}
}

func NewSlurper(processCallback ProcessMessageFunc, config *SlurperConfig) (*Slurper, error) {
	if processCallback == nil {
		return nil, fmt.Errorf("processCallback is required")
	}
	if config == nil {
		config = DefaultSlurperConfig()
	}

	s := &Slurper{
		processCallback: processCallback,
		Config:          config,
		subs:            make(map[string]*Subscription),
		shutdownChan:    make(chan bool),
		shutdownResult:  make(chan error),
		logger:          slog.Default().With("system", "slurper"),
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

func (s *Slurper) ComputeLimiterCounts(accountLimit int64, trusted bool) StreamLimiterCounts {
	if trusted {
		return StreamLimiterCounts{
			PerSecond: s.Config.TrustedPerSecondLimit,
			PerHour:   s.Config.TrustedPerHourLimit,
			PerDay:    s.Config.TrustedPerDayLimit,
		}
	}
	return StreamLimiterCounts{
		PerSecond: s.Config.BaselinePerSecondLimit + (accountLimit / 1000),
		PerHour:   s.Config.BaselinePerHourLimit + accountLimit,
		PerDay:    s.Config.BaselinePerDayLimit + accountLimit*10,
	}
}

func (s *Slurper) UpdateLimiters(hostname string, accountLimit int64, trusted bool) error {

	newLims := s.ComputeLimiterCounts(accountLimit, trusted)

	s.subsLk.Lock()
	defer s.subsLk.Unlock()

	sub, ok := s.subs[hostname]
	if !ok {
		return fmt.Errorf("updating limits for %s: %w", hostname, ErrHostInactive)
	}

	sub.Limiters.PerSecond.SetLimit(newLims.PerSecond)
	sub.Limiters.PerHour.SetLimit(newLims.PerHour)
	sub.Limiters.PerDay.SetLimit(newLims.PerDay)

	return nil
}

func (s *Slurper) GetLimits(hostname string) (*StreamLimiterCounts, error) {
	s.subsLk.Lock()
	defer s.subsLk.Unlock()

	sub, ok := s.subs[hostname]
	if !ok {
		return nil, fmt.Errorf("reading limits for %s: %w", hostname, ErrHostInactive)
	}

	slc := sub.Limiters.Counts()
	return &slc, nil
}

// Shutdown shuts down the entire Slurper (all subscriptions)
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

// high-level entry point for opening a subscription (websocket connection). This might be called when adding a new host, or when re-connecting to a previously subscribed host.
//
// NOTE: the `host` parameter (a database row) contains metadata about the host at a point in time. Subsequent changes to the database aren't reflected in that struct, and changes to the struct don't get persisted to database.
func (s *Slurper) Subscribe(host *models.Host) error {
	s.subsLk.Lock()
	defer s.subsLk.Unlock()

	_, ok := s.subs[host.Hostname]
	if ok {
		return fmt.Errorf("already subscribed: %s", host.Hostname)
	}

	counts := s.ComputeLimiterCounts(host.AccountLimit, host.Trusted)
	perSec, _ := slidingwindow.NewLimiter(time.Second, counts.PerSecond, windowFunc)
	perHour, _ := slidingwindow.NewLimiter(time.Hour, counts.PerHour, windowFunc)
	perDay, _ := slidingwindow.NewLimiter(time.Hour*24, counts.PerDay, windowFunc)
	limiters := &StreamLimiters{
		PerSecond: perSec,
		PerHour:   perHour,
		PerDay:    perDay,
	}

	ctx, cancel := context.WithCancel(context.Background())
	sub := Subscription{
		Hostname: host.Hostname,
		HostID:   host.ID,
		Limiters: limiters,
		ctx:      ctx,
		cancel:   cancel,
	}
	sub.LastSeq.Store(host.LastSeq)
	s.subs[host.Hostname] = &sub

	go s.subscribeWithRedialer(ctx, host, &sub)

	return nil
}

// Main event-loop for a subscription (websocket connection to upstream host), expected to be called as a goroutine.
//
// On connection failure (drop or failed initial connection), will attempt re-connects, with backoff.
func (s *Slurper) subscribeWithRedialer(ctx context.Context, host *models.Host, sub *Subscription) {

	logger := s.logger.With("host", host.Hostname)
	defer func() {
		s.subsLk.Lock()
		defer s.subsLk.Unlock()

		delete(s.subs, host.Hostname)
	}()

	d := websocket.Dialer{
		HandshakeTimeout: time.Second * 5,
	}

	// if this isn't a localhost / private connection, then we should enable SSRF protections
	if !host.NoSSL {
		netDialer := ssrf.PublicOnlyDialer()
		d.NetDialContext = netDialer.DialContext
	}

	cursor := host.LastSeq

	connectedInbound.Inc()
	defer connectedInbound.Dec()
	// TODO: add a metric for number of subscriptions which are attempting to reconnect

	var backoff int
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		u := host.SubscribeReposURL()
		if cursor > 0 {
			u = fmt.Sprintf("%s?cursor=%d", u, cursor)
		}
		hdr := make(http.Header)
		hdr.Add("User-Agent", s.Config.UserAgent)
		conn, resp, err := d.DialContext(ctx, u, hdr)
		if err != nil {
			logger.Warn("dialing failed", "err", err, "backoff", backoff)
			time.Sleep(sleepForBackoff(backoff))
			backoff++

			if backoff > 15 {
				logger.Warn("host does not appear to be online, disabling for now")
				if err := s.Config.PersistHostStatusCallback(ctx, sub.HostID, models.HostStatusOffline); err != nil {
					logger.Error("failed to update host status", "err", err)
				}
				return
			}

			continue
		}

		// check if we connected to a relay (eg, this indigo relay, or rainbow) and drop if so
		serverHdr := resp.Header.Get("Server")
		if strings.Contains(serverHdr, "atproto-relay") {
			logger.Warn("subscribed host is atproto relay of some kind, banning", "header", "Server", "value", serverHdr, "url", u)
			if err := s.Config.PersistHostStatusCallback(ctx, sub.HostID, models.HostStatusBanned); err != nil {
				logger.Error("failed to update host status", "err", err)
			}
			return
		}

		logger.Debug("event subscription response", "code", resp.StatusCode, "url", u)

		if err := s.handleConnection(ctx, conn, sub); err != nil {

			// TODO: measure the last N connection error times and if they're coming too fast reconnect slower or don't reconnect and wait for requestCrawl
			logger.Warn("host connection failed", "err", err, "backoff", backoff)

			// for all other errors, keep retrying / reconnecting
		}

		updatedCursor := sub.LastSeq.Load()
		if updatedCursor > cursor {
			// did we make any progress?
			cursor = updatedCursor
			backoff = 0

			// persist updated cursor
			if s.Config.PersistCursorCallback != nil {
				batch := []HostCursor{sub.HostCursor()}
				if err := s.Config.PersistCursorCallback(ctx, &batch); err != nil {
					logger.Warn("failed to persist cursor")
				}
			}
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

// Configures event processing for a websocket connection, using the parallel schedule helper library, with all events processed using the configured callback function.
func (s *Slurper) handleConnection(ctx context.Context, conn *websocket.Conn, sub *Subscription) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	rsc := &stream.RepoStreamCallbacks{
		RepoCommit: func(evt *comatproto.SyncSubscribeRepos_Commit) error {
			logger := s.logger.With("host", sub.Hostname, "did", evt.Repo, "seq", evt.Seq, "eventType", "commit")
			logger.Debug("got remote repo event")
			if err := s.processCallback(context.Background(), &stream.XRPCStreamEvent{RepoCommit: evt}, sub.Hostname, sub.HostID); err != nil {
				logger.Error("failed handling event", "err", err)
			}
			sub.UpdateSeq()

			return nil
		},
		RepoSync: func(evt *comatproto.SyncSubscribeRepos_Sync) error {
			logger := s.logger.With("host", sub.Hostname, "did", evt.Did, "seq", evt.Seq, "eventType", "sync")
			logger.Debug("commit event")
			if err := s.processCallback(context.Background(), &stream.XRPCStreamEvent{RepoSync: evt}, sub.Hostname, sub.HostID); err != nil {
				logger.Error("failed handling event", "err", err)
			}
			sub.UpdateSeq()

			return nil
		},
		RepoIdentity: func(evt *comatproto.SyncSubscribeRepos_Identity) error {
			logger := s.logger.With("host", sub.Hostname, "did", evt.Did, "seq", evt.Seq, "eventType", "identity")
			logger.Debug("identity event")
			if err := s.processCallback(context.Background(), &stream.XRPCStreamEvent{RepoIdentity: evt}, sub.Hostname, sub.HostID); err != nil {
				logger.Error("failed handling event", "err", err)
			}
			sub.UpdateSeq()

			return nil
		},
		RepoAccount: func(evt *comatproto.SyncSubscribeRepos_Account) error {
			logger := s.logger.With("host", sub.Hostname, "did", evt.Did, "seq", evt.Seq, "eventType", "account")
			s.logger.Debug("account event")
			if err := s.processCallback(context.Background(), &stream.XRPCStreamEvent{RepoAccount: evt}, sub.Hostname, sub.HostID); err != nil {
				logger.Error("failed handling event", "err", err)
			}
			sub.UpdateSeq()

			return nil
		},
		Error: func(evt *stream.ErrorFrame) error {
			logger := s.logger.With("host", sub.Hostname)
			logger.Warn("error event from upstream", "name", evt.Error, "message", evt.Message)
			switch evt.Error {
			case "FutureCursor":
				if err := s.Config.PersistHostStatusCallback(ctx, sub.HostID, models.HostStatusIdle); err != nil {
					logger.Error("failed updating host status due to future cursor", "err", err)
				}
				logger.Warn("dropping connection to host due to future cursor")
				sub.cancel()
				return ErrFutureCursor
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
			if err := s.processCallback(context.Background(), &stream.XRPCStreamEvent{RepoHandle: evt}, sub.Hostname, sub.HostID); err != nil {
				logger.Error("failed handling event", "err", err)
			}
			sub.UpdateSeq()
			return nil
		},
		RepoMigrate: func(evt *comatproto.SyncSubscribeRepos_Migrate) error { // DEPRECATED
			logger := s.logger.With("host", sub.Hostname, "did", evt.Did, "seq", evt.Seq, "eventType", "migrate")
			logger.Debug("got remote repo migrate event", "migrateTo", evt.MigrateTo)
			if err := s.processCallback(context.Background(), &stream.XRPCStreamEvent{RepoMigrate: evt}, sub.Hostname, sub.HostID); err != nil {
				logger.Error("failed handling event", "err", err)
			}
			sub.UpdateSeq()
			return nil
		},
		RepoTombstone: func(evt *comatproto.SyncSubscribeRepos_Tombstone) error { // DEPRECATED
			logger := s.logger.With("host", sub.Hostname, "did", evt.Did, "seq", evt.Seq, "eventType", "tombstone")
			logger.Debug("got remote repo tombstone event")
			if err := s.processCallback(context.Background(), &stream.XRPCStreamEvent{RepoTombstone: evt}, sub.Hostname, sub.HostID); err != nil {
				logger.Error("failed handling event", "err", err)
			}
			sub.UpdateSeq()
			return nil
		},
	}

	limiters := []*slidingwindow.Limiter{
		sub.Limiters.PerSecond,
		sub.Limiters.PerHour,
		sub.Limiters.PerDay,
	}

	// NOTE: `InstrumentedRepoStreamCallbacks` is where event limiters get called/enforced
	instrumentedRSC := stream.NewInstrumentedRepoStreamCallbacks(limiters, rsc.EventHandler)

	sub.scheduler = parallel.NewScheduler(
		s.Config.ConcurrencyPerHost,
		s.Config.QueueDepthPerHost,
		conn.RemoteAddr().String(),
		instrumentedRSC.EventHandler,
	)
	connLogger := s.logger.With("host", sub.Hostname)
	return stream.HandleRepoStream(ctx, conn, sub.scheduler, connLogger)
}

type HostCursor struct {
	HostID  uint64
	LastSeq int64
}

// persistCursors sends all cursors to callback to be persisted in database (if registered)
func (s *Slurper) persistCursors(ctx context.Context) error {
	if s.Config.PersistCursorCallback == nil {
		s.logger.Warn("skipping cursor persist because no PersistCursorCallback registered")
		return nil
	}
	start := time.Now()

	// gather cursors: lock overall set, then lock each individual subscription while gathering
	s.subsLk.Lock()
	cursors := make([]HostCursor, len(s.subs))
	i := 0
	for _, sub := range s.subs {
		sub.UpdateSeq()
		cursors[i] = sub.HostCursor()
		i++
	}
	s.subsLk.Unlock()

	err := s.Config.PersistCursorCallback(ctx, &cursors)
	s.logger.Info("finished persisting cursors", "count", len(cursors), "duration", time.Since(start).String(), "err", err)
	return err
}

// gets a snapshot of current subsription hostnames
func (s *Slurper) GetActiveSubHostnames() []string {
	s.subsLk.Lock()
	defer s.subsLk.Unlock()

	var keys []string
	for k := range s.subs {
		keys = append(keys, k)
	}
	return keys
}

func (s *Slurper) KillUpstreamConnection(ctx context.Context, hostname string, ban bool) error {
	s.subsLk.Lock()
	defer s.subsLk.Unlock()

	sub, ok := s.subs[hostname]
	if !ok {
		return fmt.Errorf("killing connection %q: %w", hostname, ErrHostInactive)
	}
	sub.cancel()

	if ban && s.Config.PersistHostStatusCallback != nil {
		if err := s.Config.PersistHostStatusCallback(ctx, sub.HostID, models.HostStatusBanned); err != nil {
			return fmt.Errorf("failed to set host as banned: %w", err)
		}
	}

	return nil
}
