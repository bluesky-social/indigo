package eventmgr

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/models"
	"github.com/prometheus/client_golang/prometheus"

	"go.opentelemetry.io/otel"
)

var (
	ErrPlaybackShutdown = fmt.Errorf("playback shutting down")
	ErrCaughtUp         = fmt.Errorf("caught up")
)

type EventManager struct {
	subs   []*Subscriber
	subsLk sync.Mutex

	bufferSize          int
	crossoverBufferSize int

	persister events.EventPersistence

	log *slog.Logger
}

type Subscriber struct {
	outgoing chan *events.XRPCStreamEvent

	filter func(*events.XRPCStreamEvent) bool

	done chan struct{}

	cleanup func()

	lk        sync.Mutex
	cleanedUp bool

	ident            string
	enqueuedCounter  prometheus.Counter
	broadcastCounter prometheus.Counter
}

func NewEventManager(persister events.EventPersistence) *EventManager {
	em := &EventManager{
		bufferSize:          16 << 10,
		crossoverBufferSize: 512,
		persister:           persister,
		log:                 slog.Default().With("system", "events"),
	}

	persister.SetEventBroadcaster(em.broadcastEvent)

	return em
}

func (em *EventManager) Shutdown(ctx context.Context) error {
	return em.persister.Shutdown(ctx)
}

func (em *EventManager) broadcastEvent(evt *events.XRPCStreamEvent) {
	// the main thing we do is send it out, so MarshalCBOR once
	if err := evt.Preserialize(); err != nil {
		em.log.Error("broadcast serialize failed", "err", err)
		// serialize isn't going to go better later, this event is cursed
		return
	}

	em.subsLk.Lock()
	defer em.subsLk.Unlock()

	// TODO: for a larger fanout we should probably have dedicated goroutines
	// for subsets of the subscriber set, and tiered channels to distribute
	// events out to them, or some similar architecture
	// Alternatively, we might just want to not allow too many subscribers
	// directly to the bgs, and have rebroadcasting proxies instead
	for _, s := range em.subs {
		if s.filter(evt) {
			s.enqueuedCounter.Inc()
			select {
			case s.outgoing <- evt:
			case <-s.done:
			default:
				// filter out all future messages that would be
				// sent to this subscriber, but wait for it to
				// actually be removed by the correct bit of
				// code
				s.filter = func(*events.XRPCStreamEvent) bool { return false }

				em.log.Warn("dropping slow consumer due to event overflow", "bufferSize", len(s.outgoing), "ident", s.ident)
				go func(torem *Subscriber) {
					torem.lk.Lock()
					if !torem.cleanedUp {
						select {
						case torem.outgoing <- &events.XRPCStreamEvent{
							Error: &events.ErrorFrame{
								Error: "ConsumerTooSlow",
							},
						}:
						case <-time.After(time.Second * 5):
							em.log.Warn("failed to send error frame to backed up consumer", "ident", torem.ident)
						}
					}
					torem.lk.Unlock()
					torem.cleanup()
				}(s)
			}
			s.broadcastCounter.Inc()
		}
	}
}

func (em *EventManager) persistAndSendEvent(ctx context.Context, evt *events.XRPCStreamEvent) {
	// TODO: can cut 5-10% off of disk persister benchmarks by making this function
	// accept a uid. The lookup inside the persister is notably expensive (despite
	// being an lru cache?)
	if err := em.persister.Persist(ctx, evt); err != nil {
		em.log.Error("failed to persist outbound event", "err", err)
	}
}

func (em *EventManager) Subscribe(ctx context.Context, ident string, filter func(*events.XRPCStreamEvent) bool, since *int64) (<-chan *events.XRPCStreamEvent, func(), error) {
	if filter == nil {
		filter = func(*events.XRPCStreamEvent) bool { return true }
	}

	done := make(chan struct{})
	sub := &Subscriber{
		ident:            ident,
		outgoing:         make(chan *events.XRPCStreamEvent, em.bufferSize),
		filter:           filter,
		done:             done,
		enqueuedCounter:  eventsEnqueued.WithLabelValues(ident),
		broadcastCounter: eventsBroadcast.WithLabelValues(ident),
	}

	sub.cleanup = sync.OnceFunc(func() {
		sub.lk.Lock()
		defer sub.lk.Unlock()
		close(done)
		em.rmSubscriber(sub)
		close(sub.outgoing)
		sub.cleanedUp = true
	})

	if since == nil {
		em.addSubscriber(sub)
		return sub.outgoing, sub.cleanup, nil
	}

	out := make(chan *events.XRPCStreamEvent, em.crossoverBufferSize)

	go func() {
		lastSeq := *since
		// run playback to get through *most* of the events, getting our current cursor close to realtime
		if err := em.persister.Playback(ctx, *since, func(e *events.XRPCStreamEvent) error {
			select {
			case <-done:
				return ErrPlaybackShutdown
			case out <- e:
				seq := events.SequenceForEvent(e)
				if seq > 0 {
					lastSeq = seq
				}
				return nil
			}
		}); err != nil {
			if errors.Is(err, ErrPlaybackShutdown) {
				em.log.Warn("events playback", "err", err)
			} else {
				em.log.Error("events playback", "err", err)
			}

			// TODO: send an error frame or something?
			close(out)
			return
		}

		// now, start buffering events from the live stream
		em.addSubscriber(sub)

		first := <-sub.outgoing

		// run playback again to get us to the events that have started buffering
		if err := em.persister.Playback(ctx, lastSeq, func(e *events.XRPCStreamEvent) error {
			seq := events.SequenceForEvent(e)
			if seq > events.SequenceForEvent(first) {
				return ErrCaughtUp
			}

			select {
			case <-done:
				return ErrPlaybackShutdown
			case out <- e:
				return nil
			}
		}); err != nil {
			if !errors.Is(err, ErrCaughtUp) {
				em.log.Error("events playback", "err", err)

				// TODO: send an error frame or something?
				close(out)
				em.rmSubscriber(sub)
				return
			}
		}

		// now that we are caught up, just copy events from the channel over
		for evt := range sub.outgoing {
			select {
			case out <- evt:
			case <-done:
				em.rmSubscriber(sub)
				return
			}
		}
	}()

	return out, sub.cleanup, nil
}

func (em *EventManager) AddEvent(ctx context.Context, ev *events.XRPCStreamEvent) error {
	ctx, span := otel.Tracer("events").Start(ctx, "AddEvent")
	defer span.End()

	em.persistAndSendEvent(ctx, ev)
	return nil
}

func (em *EventManager) rmSubscriber(sub *Subscriber) {
	em.subsLk.Lock()
	defer em.subsLk.Unlock()

	for i, s := range em.subs {
		if s == sub {
			em.subs[i] = em.subs[len(em.subs)-1]
			em.subs = em.subs[:len(em.subs)-1]
			break
		}
	}
}

func (em *EventManager) addSubscriber(sub *Subscriber) {
	em.subsLk.Lock()
	defer em.subsLk.Unlock()

	em.subs = append(em.subs, sub)
}

func (em *EventManager) TakeDownRepo(ctx context.Context, user models.Uid) error {
	return em.persister.TakeDownRepo(ctx, user)
}
