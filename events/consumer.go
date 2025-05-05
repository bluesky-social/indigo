package events

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/RussellLuo/slidingwindow"
	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/carlmjohnson/versioninfo"
	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus"
)

type RepoStreamCallbacks struct {
	RepoCommit    func(evt *comatproto.SyncSubscribeRepos_Commit) error
	RepoSync      func(evt *comatproto.SyncSubscribeRepos_Sync) error
	RepoHandle    func(evt *comatproto.SyncSubscribeRepos_Handle) error
	RepoIdentity  func(evt *comatproto.SyncSubscribeRepos_Identity) error
	RepoAccount   func(evt *comatproto.SyncSubscribeRepos_Account) error
	RepoInfo      func(evt *comatproto.SyncSubscribeRepos_Info) error
	RepoMigrate   func(evt *comatproto.SyncSubscribeRepos_Migrate) error
	RepoTombstone func(evt *comatproto.SyncSubscribeRepos_Tombstone) error
	LabelLabels   func(evt *comatproto.LabelSubscribeLabels_Labels) error
	LabelInfo     func(evt *comatproto.LabelSubscribeLabels_Info) error
	Error         func(evt *ErrorFrame) error
}

func (rsc *RepoStreamCallbacks) EventHandler(ctx context.Context, xev *XRPCStreamEvent) error {
	switch {
	case xev.RepoCommit != nil && rsc.RepoCommit != nil:
		return rsc.RepoCommit(xev.RepoCommit)
	case xev.RepoSync != nil && rsc.RepoSync != nil:
		return rsc.RepoSync(xev.RepoSync)
	case xev.RepoHandle != nil && rsc.RepoHandle != nil:
		return rsc.RepoHandle(xev.RepoHandle)
	case xev.RepoInfo != nil && rsc.RepoInfo != nil:
		return rsc.RepoInfo(xev.RepoInfo)
	case xev.RepoMigrate != nil && rsc.RepoMigrate != nil:
		return rsc.RepoMigrate(xev.RepoMigrate)
	case xev.RepoIdentity != nil && rsc.RepoIdentity != nil:
		return rsc.RepoIdentity(xev.RepoIdentity)
	case xev.RepoAccount != nil && rsc.RepoAccount != nil:
		return rsc.RepoAccount(xev.RepoAccount)
	case xev.RepoTombstone != nil && rsc.RepoTombstone != nil:
		return rsc.RepoTombstone(xev.RepoTombstone)
	case xev.LabelLabels != nil && rsc.LabelLabels != nil:
		return rsc.LabelLabels(xev.LabelLabels)
	case xev.LabelInfo != nil && rsc.LabelInfo != nil:
		return rsc.LabelInfo(xev.LabelInfo)
	case xev.Error != nil && rsc.Error != nil:
		return rsc.Error(xev.Error)
	default:
		return nil
	}
}

type InstrumentedRepoStreamCallbacks struct {
	limiters []*slidingwindow.Limiter
	Next     func(ctx context.Context, xev *XRPCStreamEvent) error
}

func NewInstrumentedRepoStreamCallbacks(limiters []*slidingwindow.Limiter, next func(ctx context.Context, xev *XRPCStreamEvent) error) *InstrumentedRepoStreamCallbacks {
	return &InstrumentedRepoStreamCallbacks{
		limiters: limiters,
		Next:     next,
	}
}

func waitForLimiter(ctx context.Context, lim *slidingwindow.Limiter) error {
	if lim.Allow() {
		return nil
	}

	// wait until the limiter is ready (check every 100ms)
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()

	for !lim.Allow() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}

	return nil
}

func (rsc *InstrumentedRepoStreamCallbacks) EventHandler(ctx context.Context, xev *XRPCStreamEvent) error {
	// Wait on all limiters before calling the next handler
	for _, lim := range rsc.limiters {
		if err := waitForLimiter(ctx, lim); err != nil {
			return err
		}
	}
	return rsc.Next(ctx, xev)
}

type instrumentedReader struct {
	r            io.Reader
	addr         string
	bytesCounter prometheus.Counter
}

func (sr *instrumentedReader) Read(p []byte) (int, error) {
	n, err := sr.r.Read(p)
	sr.bytesCounter.Add(float64(n))
	return n, err
}

// Options that control how we should subscribe to a remote event stream
type HandleRepoStreamRobustOptions struct {
	// Controls how work should be processed when reading from the remote
	Scheduler Scheduler

	// The base URL (scheme and port) to which we will connect (i.e. `wss://example.com:8080`)
	Upstream string

	// The cursor to use when dialing the remote repo (optional)
	Cursor *int

	// The dialer user to establish the websocket connection
	Dialer *websocket.Dialer

	// The list of HTTP headers to pass when establishing the websocket connection. If users do
	// not supply a `User-Agent`, a default will be provided.
	Header http.Header

	// The maximum number of consecutive reconnects to try before shutting down
	MaxReconnectAttempts int

	// Uses the default logger if none is provided
	Log *slog.Logger
}

// Constructs an options object with a set of sane default arguments that can be overridden by the user
func DefaultHandleRepoStreamRobustOptions(sched Scheduler, host string) *HandleRepoStreamRobustOptions {
	return &HandleRepoStreamRobustOptions{
		Scheduler:            sched,
		Upstream:             host,
		Dialer:               websocket.DefaultDialer,
		Header:               http.Header{},
		MaxReconnectAttempts: 10,
		Log:                  slog.Default().With("system", "events"),
	}
}

// The same as `HandleRepoStream`, but with auto-reconnects in the case of upstream disconnects
func HandleRepoStreamRobust(ctx context.Context, opts *HandleRepoStreamRobustOptions) error {
	if opts == nil || opts.Scheduler == nil || opts.Upstream == "" {
		return fmt.Errorf("invalid HandleRepoStreamRobust options")
	}

	// Set defaults if not provided
	if opts.Dialer == nil {
		opts.Dialer = websocket.DefaultDialer
	}
	if opts.Header == nil {
		opts.Header = http.Header{}
	}
	if opts.Header.Get("User-Agent") == "" {
		opts.Header.Set("User-Agent", "indigo/"+versioninfo.Short())
	}

	upstream, err := url.Parse(opts.Upstream)
	if err != nil {
		return fmt.Errorf("invalid upstream url %q: %w", opts.Upstream, err)
	}
	upstream = upstream.JoinPath("/xrpc/com.atproto.sync.subscribeRepos")

	if opts.Cursor != nil {
		upstream.RawQuery = fmt.Sprintf("cursor=%d", *opts.Cursor)
	}

	retryableCloses := []int{
		websocket.CloseNormalClosure,
		websocket.CloseGoingAway,
		websocket.CloseAbnormalClosure,
	}

	failures := 0
	for failures <= opts.MaxReconnectAttempts {
		con, _, err := opts.Dialer.DialContext(ctx, upstream.String(), opts.Header)
		if err != nil {
			if failures >= opts.MaxReconnectAttempts {
				return fmt.Errorf("failed to dial host (max retries exceeded): %w", err)
			}

			time.Sleep(backoffDuration(failures))
			failures++
			continue
		}

		msgRead := false
		err = handleRepoStream(ctx, con, opts.Scheduler, opts.Log, &msgRead)
		if msgRead {
			// reset the failure counter
			failures = 0
		}
		if websocket.IsCloseError(err, retryableCloses...) {
			if failures >= opts.MaxReconnectAttempts {
				return fmt.Errorf("failed to : %w", err)
			}

			time.Sleep(backoffDuration(failures))
			failures++
			continue
		}
		if err != nil {
			// non-retryable error
			return err
		}
	}

	return nil
}

func backoffDuration(retry int) time.Duration {
	baseMS := float64(50)

	// Exponential backoff: 2^retry * base
	durationMS := math.Pow(2, float64(retry)) * baseMS

	// Clamp
	durationMS = min(1000, durationMS)

	// Add jitter
	jitter := durationMS * (0.25 + rand.Float64())

	return time.Duration(jitter) * time.Millisecond
}

// HandleRepoStream
// con is source of events
// sched gets AddWork for each event
// log may be nil for default logger
func HandleRepoStream(ctx context.Context, con *websocket.Conn, sched Scheduler, log *slog.Logger) error {
	return handleRepoStream(ctx, con, sched, log, nil)
}

func handleRepoStream(ctx context.Context, con *websocket.Conn, sched Scheduler, log *slog.Logger, msgRead *bool) error {
	if log == nil {
		log = slog.Default().With("system", "events")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer sched.Shutdown()

	remoteAddr := con.RemoteAddr().String()

	go func() {
		t := time.NewTicker(time.Second * 30)
		defer t.Stop()
		failcount := 0

		for {

			select {
			case <-t.C:
				if err := con.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(time.Second*10)); err != nil {
					log.Warn("failed to ping", "err", err)
					failcount++
					if failcount >= 4 {
						log.Error("too many ping fails", "count", failcount)
						con.Close()
						return
					}
				} else {
					failcount = 0 // ok ping
				}
			case <-ctx.Done():
				con.Close()
				return
			}
		}
	}()

	con.SetPingHandler(func(message string) error {
		err := con.WriteControl(websocket.PongMessage, []byte(message), time.Now().Add(time.Second*60))
		if err == websocket.ErrCloseSent {
			return nil
		} else if e, ok := err.(net.Error); ok && e.Temporary() {
			return nil
		}
		return err
	})

	con.SetPongHandler(func(_ string) error {
		if err := con.SetReadDeadline(time.Now().Add(time.Minute)); err != nil {
			log.Error("failed to set read deadline", "err", err)
		}

		return nil
	})

	lastSeq := int64(-1)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		mt, rawReader, err := con.NextReader()
		if err != nil {
			return fmt.Errorf("con err at read: %w", err)
		}
		if msgRead != nil {
			*msgRead = true
		}

		switch mt {
		default:
			return fmt.Errorf("expected binary message from subscription endpoint")
		case websocket.BinaryMessage:
			// ok
		}

		r := &instrumentedReader{
			r:            rawReader,
			addr:         remoteAddr,
			bytesCounter: bytesFromStreamCounter.WithLabelValues(remoteAddr),
		}

		var header EventHeader
		if err := header.UnmarshalCBOR(r); err != nil {
			return fmt.Errorf("reading header: %w", err)
		}

		eventsFromStreamCounter.WithLabelValues(remoteAddr).Inc()

		switch header.Op {
		case EvtKindMessage:
			switch header.MsgType {
			case "#commit":
				var evt comatproto.SyncSubscribeRepos_Commit
				if err := evt.UnmarshalCBOR(r); err != nil {
					return fmt.Errorf("reading repoCommit event: %w", err)
				}

				if evt.Seq < lastSeq {
					log.Error("Got events out of order from stream", "seq", evt.Seq, "prev", lastSeq)
				}

				lastSeq = evt.Seq

				if err := sched.AddWork(ctx, evt.Repo, &XRPCStreamEvent{
					RepoCommit: &evt,
				}); err != nil {
					return err
				}
			case "#sync":
				var evt comatproto.SyncSubscribeRepos_Sync
				if err := evt.UnmarshalCBOR(r); err != nil {
					return fmt.Errorf("reading repoSync event: %w", err)
				}

				if evt.Seq < lastSeq {
					log.Error("Got events out of order from stream", "seq", evt.Seq, "prev", lastSeq)
				}

				lastSeq = evt.Seq

				if err := sched.AddWork(ctx, evt.Did, &XRPCStreamEvent{
					RepoSync: &evt,
				}); err != nil {
					return err
				}
			case "#handle":
				// TODO: DEPRECATED message; warning/counter; drop message
				var evt comatproto.SyncSubscribeRepos_Handle
				if err := evt.UnmarshalCBOR(r); err != nil {
					return err
				}

				if evt.Seq < lastSeq {
					log.Error("Got events out of order from stream", "seq", evt.Seq, "prev", lastSeq)
				}
				lastSeq = evt.Seq

				if err := sched.AddWork(ctx, evt.Did, &XRPCStreamEvent{
					RepoHandle: &evt,
				}); err != nil {
					return err
				}
			case "#identity":
				var evt comatproto.SyncSubscribeRepos_Identity
				if err := evt.UnmarshalCBOR(r); err != nil {
					return err
				}

				if evt.Seq < lastSeq {
					log.Error("Got events out of order from stream", "seq", evt.Seq, "prev", lastSeq)
				}
				lastSeq = evt.Seq

				if err := sched.AddWork(ctx, evt.Did, &XRPCStreamEvent{
					RepoIdentity: &evt,
				}); err != nil {
					return err
				}
			case "#account":
				var evt comatproto.SyncSubscribeRepos_Account
				if err := evt.UnmarshalCBOR(r); err != nil {
					return err
				}

				if evt.Seq < lastSeq {
					log.Error("Got events out of order from stream", "seq", evt.Seq, "prev", lastSeq)
				}
				lastSeq = evt.Seq

				if err := sched.AddWork(ctx, evt.Did, &XRPCStreamEvent{
					RepoAccount: &evt,
				}); err != nil {
					return err
				}
			case "#info":
				// TODO: this might also be a LabelInfo (as opposed to RepoInfo)
				var evt comatproto.SyncSubscribeRepos_Info
				if err := evt.UnmarshalCBOR(r); err != nil {
					return err
				}

				if err := sched.AddWork(ctx, "", &XRPCStreamEvent{
					RepoInfo: &evt,
				}); err != nil {
					return err
				}
			case "#migrate":
				// TODO: DEPRECATED message; warning/counter; drop message
				var evt comatproto.SyncSubscribeRepos_Migrate
				if err := evt.UnmarshalCBOR(r); err != nil {
					return err
				}

				if evt.Seq < lastSeq {
					log.Error("Got events out of order from stream", "seq", evt.Seq, "prev", lastSeq)
				}
				lastSeq = evt.Seq

				if err := sched.AddWork(ctx, evt.Did, &XRPCStreamEvent{
					RepoMigrate: &evt,
				}); err != nil {
					return err
				}
			case "#tombstone":
				// TODO: DEPRECATED message; warning/counter; drop message
				var evt comatproto.SyncSubscribeRepos_Tombstone
				if err := evt.UnmarshalCBOR(r); err != nil {
					return err
				}

				if evt.Seq < lastSeq {
					log.Error("Got events out of order from stream", "seq", evt.Seq, "prev", lastSeq)
				}
				lastSeq = evt.Seq

				if err := sched.AddWork(ctx, evt.Did, &XRPCStreamEvent{
					RepoTombstone: &evt,
				}); err != nil {
					return err
				}
			case "#labels":
				var evt comatproto.LabelSubscribeLabels_Labels
				if err := evt.UnmarshalCBOR(r); err != nil {
					return fmt.Errorf("reading Labels event: %w", err)
				}

				if evt.Seq < lastSeq {
					log.Error("Got events out of order from stream", "seq", evt.Seq, "prev", lastSeq)
				}

				lastSeq = evt.Seq

				if err := sched.AddWork(ctx, "", &XRPCStreamEvent{
					LabelLabels: &evt,
				}); err != nil {
					return err
				}
			}

		case EvtKindErrorFrame:
			var errframe ErrorFrame
			if err := errframe.UnmarshalCBOR(r); err != nil {
				return err
			}

			if err := sched.AddWork(ctx, "", &XRPCStreamEvent{
				Error: &errframe,
			}); err != nil {
				return err
			}

		default:
			return fmt.Errorf("unrecognized event stream type: %d", header.Op)
		}

	}
}
