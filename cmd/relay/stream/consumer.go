package stream

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"time"

	comatproto "github.com/gander-social/gander-indigo-sovereign/api/atproto"

	"github.com/RussellLuo/slidingwindow"
	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus"
)

const MaxMessageBytes = 5_000_000

type RepoStreamCallbacks struct {
	RepoCommit   func(evt *comatproto.SyncSubscribeRepos_Commit) error
	RepoSync     func(evt *comatproto.SyncSubscribeRepos_Sync) error
	RepoIdentity func(evt *comatproto.SyncSubscribeRepos_Identity) error
	RepoAccount  func(evt *comatproto.SyncSubscribeRepos_Account) error
	RepoInfo     func(evt *comatproto.SyncSubscribeRepos_Info) error
	LabelLabels  func(evt *comatproto.LabelSubscribeLabels_Labels) error
	LabelInfo    func(evt *comatproto.LabelSubscribeLabels_Info) error
	Error        func(evt *ErrorFrame) error
}

func (rsc *RepoStreamCallbacks) EventHandler(ctx context.Context, xev *XRPCStreamEvent) error {
	switch {
	case xev.RepoCommit != nil && rsc.RepoCommit != nil:
		return rsc.RepoCommit(xev.RepoCommit)
	case xev.RepoSync != nil && rsc.RepoSync != nil:
		return rsc.RepoSync(xev.RepoSync)
	case xev.RepoInfo != nil && rsc.RepoInfo != nil:
		return rsc.RepoInfo(xev.RepoInfo)
	case xev.RepoIdentity != nil && rsc.RepoIdentity != nil:
		return rsc.RepoIdentity(xev.RepoIdentity)
	case xev.RepoAccount != nil && rsc.RepoAccount != nil:
		return rsc.RepoAccount(xev.RepoAccount)
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

// HandleRepoStream
// con is source of events
// sched gets AddWork for each event
// logger may be nil for default logger
func HandleRepoStream(ctx context.Context, con *websocket.Conn, sched Scheduler, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default().With("system", "events")
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
					logger.Warn("failed to ping", "err", err)
					failcount++
					if failcount >= 4 {
						logger.Error("too many ping fails", "count", failcount)
						_ = con.Close()
						return
					}
				} else {
					failcount = 0 // ok ping
				}
			case <-ctx.Done():
				_ = con.Close()
				return
			}
		}
	}()

	// global maximum WebSocket message size; connection will drop if exceeded
	con.SetReadLimit(MaxMessageBytes)

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
			logger.Error("failed to set read deadline", "err", err)
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

				if evt.Seq <= lastSeq {
					logger.Error("got events out of order from stream", "seq", evt.Seq, "prev", lastSeq)
					continue
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

				if evt.Seq <= lastSeq {
					logger.Error("got events out of order from stream", "seq", evt.Seq, "prev", lastSeq)
					continue
				}

				lastSeq = evt.Seq

				if err := sched.AddWork(ctx, evt.Did, &XRPCStreamEvent{
					RepoSync: &evt,
				}); err != nil {
					return err
				}
			case "#identity":
				var evt comatproto.SyncSubscribeRepos_Identity
				if err := evt.UnmarshalCBOR(r); err != nil {
					return err
				}

				if evt.Seq <= lastSeq {
					logger.Error("got events out of order from stream", "seq", evt.Seq, "prev", lastSeq)
					continue
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

				if evt.Seq <= lastSeq {
					logger.Error("got events out of order from stream", "seq", evt.Seq, "prev", lastSeq)
					continue
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
			case "#labels":
				var evt comatproto.LabelSubscribeLabels_Labels
				if err := evt.UnmarshalCBOR(r); err != nil {
					return fmt.Errorf("reading Labels event: %w", err)
				}

				if evt.Seq <= lastSeq {
					logger.Error("got events out of order from stream", "seq", evt.Seq, "prev", lastSeq)
					continue
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
