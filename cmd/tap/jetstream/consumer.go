package jetstream

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"time"

	"github.com/RussellLuo/slidingwindow"
	"github.com/bluesky-social/indigo/events"
	"github.com/gorilla/websocket"
	"github.com/klauspost/compress/zstd"
	"github.com/prometheus/client_golang/prometheus"
)

//go:embed zstd_dictionary
var ZSTDDictionary []byte

func NewZSTDDecoder() (*zstd.Decoder, error) {
	dec, err := zstd.NewReader(nil, zstd.WithDecoderDicts(ZSTDDictionary))
	if err != nil {
		return nil, fmt.Errorf("failed to create zstd decoder: %w", err)
	}
	return dec, nil
}

type RepoJetStreamCallbacks struct {
	Commit   func(evt *JetstreamEvent) error
	Sync     func(evt *JetstreamEvent) error
	Identity func(evt *JetstreamEvent) error
	Account  func(evt *JetstreamEvent) error
}

func (rsc *RepoJetStreamCallbacks) EventHandler(ctx context.Context, xev *JetstreamEvent) error {
	switch {
	case xev.Commit != nil && rsc.Commit != nil:
		return rsc.Commit(xev)
	case xev.Sync != nil && rsc.Sync != nil:
		return rsc.Sync(xev)
	case xev.Identity != nil && rsc.Identity != nil:
		return rsc.Identity(xev)
	case xev.Account != nil && rsc.Account != nil:
		return rsc.Account(xev)
	default:
		return nil
	}
}

type InstrumentedRepoStreamCallbacks struct {
	limiters []*slidingwindow.Limiter
	Next     func(ctx context.Context, xev *events.XRPCStreamEvent) error
}

func NewInstrumentedRepoStreamCallbacks(limiters []*slidingwindow.Limiter, next func(ctx context.Context, xev *events.XRPCStreamEvent) error) *InstrumentedRepoStreamCallbacks {
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

func (rsc *InstrumentedRepoStreamCallbacks) EventHandler(ctx context.Context, xev *events.XRPCStreamEvent) error {
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

// Copy of HandleRepoStream, adapted for JetStream
// con is source of events
// sched gets AddWork for each event
// log may be nil for default logger
func HandleJetStream(ctx context.Context, con *websocket.Conn, sched *Scheduler, log *slog.Logger) error {
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

	decoder, err := NewZSTDDecoder()
	if err != nil {
		slog.Error("listener: zstd decoder", "err", err)
		return err
	}
	defer decoder.Close()

	lastSeq := int64(-1)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		_, message, err := con.ReadMessage()
		if err != nil {
			return fmt.Errorf("con err at read: %w", err)
		}

		decoded, err := decoder.DecodeAll(message, nil)
		if err != nil {
			slog.Error("listener: decompress", "err", err)
			continue
		}

		var event JetstreamEvent
		if err := json.Unmarshal(decoded, &event); err != nil {
			slog.Error("listener: unmarshal", "err", err)
			continue
		}

		jetstreamEventsFromStreamCounter.WithLabelValues(remoteAddr).Inc()

		switch event.Kind {
		case EventKindCommit:
			if event.Commit == nil {
				log.Error("Got nil commit event from stream")
				continue
			}
			if event.TimeUS < lastSeq {
				log.Error("Got events out of order from stream", "time_us", event.TimeUS, "prev", lastSeq)
			}

			lastSeq = event.TimeUS

			if err := sched.AddWork(ctx, event.Did, &event); err != nil {
				return err
			}
		case EventKindSync:
			if event.Sync == nil {
				log.Error("Got nil sync event from stream")
				continue
			}
			if event.TimeUS < lastSeq {
				log.Error("Got events out of order from stream", "time_us", event.TimeUS, "prev", lastSeq)
			}

			lastSeq = event.TimeUS

			if err := sched.AddWork(ctx, event.Did, &event); err != nil {
				return err
			}
		case EventKindIdentity:
			if event.Identity == nil {
				log.Error("Got nil identity event from stream")
				continue
			}

			if event.TimeUS < lastSeq {
				log.Error("Got events out of order from stream", "time_us", event.TimeUS, "prev", lastSeq)
			}
			lastSeq = event.TimeUS

			if err := sched.AddWork(ctx, event.Did, &event); err != nil {
				return err
			}
		case EventKindAccount:
			if event.Account == nil {
				log.Error("Got nil account event from stream")
				continue
			}

			if event.TimeUS < lastSeq {
				log.Error("Got events out of order from stream", "time_us", event.TimeUS, "prev", lastSeq)
			}
			lastSeq = event.TimeUS

			if err := sched.AddWork(ctx, event.Did, &event); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unrecognized event type: %v", event.Kind)

		}
	}
}
