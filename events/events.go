package events

import (
	"context"
	"errors"
	"fmt"
	"sync"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	label "github.com/bluesky-social/indigo/api/label"
	"github.com/bluesky-social/indigo/models"
	"github.com/prometheus/client_golang/prometheus"

	logging "github.com/ipfs/go-log"
	"go.opentelemetry.io/otel"
)

var log = logging.Logger("events")

type Scheduler interface {
	AddWork(ctx context.Context, repo string, val *XRPCStreamEvent) error
	Shutdown()
}

type EventManager struct {
	subs   []*Subscriber
	subsLk sync.Mutex

	bufferSize int

	persister EventPersistence
}

func NewEventManager(persister EventPersistence) *EventManager {
	em := &EventManager{
		bufferSize: 32 << 10,
		persister:  persister,
	}

	persister.SetEventBroadcaster(em.broadcastEvent)

	return em
}

const (
	opSubscribe = iota
	opUnsubscribe
	opSend
)

type Operation struct {
	op  int
	sub *Subscriber
	evt *XRPCStreamEvent
}

func (em *EventManager) Shutdown(ctx context.Context) error {
	return em.persister.Shutdown(ctx)
}

func (em *EventManager) broadcastEvent(evt *XRPCStreamEvent) {
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
				go func(torem *Subscriber) {
					em.rmSubscriber(torem)
				}(s)
			default:
				log.Warnf("event overflow (%d)", len(s.outgoing))
			}
			s.broadcastCounter.Inc()
		}
	}
}

func (em *EventManager) persistAndSendEvent(ctx context.Context, evt *XRPCStreamEvent) {
	// TODO: can cut 5-10% off of disk persister benchmarks by making this function
	// accept a uid. The lookup inside the persister is notably expensive (despite
	// being an lru cache?)
	if err := em.persister.Persist(ctx, evt); err != nil {
		log.Errorf("failed to persist outbound event: %s", err)
	}
}

type Subscriber struct {
	outgoing chan *XRPCStreamEvent

	filter func(*XRPCStreamEvent) bool

	done chan struct{}

	ident            string
	enqueuedCounter  prometheus.Counter
	broadcastCounter prometheus.Counter
}

const (
	EvtKindErrorFrame = -1
	EvtKindMessage    = 1
)

type EventHeader struct {
	Op      int64  `cborgen:"op"`
	MsgType string `cborgen:"t"`
}

type XRPCStreamEvent struct {
	Error         *ErrorFrame
	RepoCommit    *comatproto.SyncSubscribeRepos_Commit
	RepoHandle    *comatproto.SyncSubscribeRepos_Handle
	RepoInfo      *comatproto.SyncSubscribeRepos_Info
	RepoMigrate   *comatproto.SyncSubscribeRepos_Migrate
	RepoTombstone *comatproto.SyncSubscribeRepos_Tombstone
	LabelLabels   *label.SubscribeLabels_Labels
	LabelInfo     *label.SubscribeLabels_Info

	// some private fields for internal routing perf
	PrivUid         models.Uid `json:"-" cborgen:"-"`
	PrivPdsId       uint       `json:"-" cborgen:"-"`
	PrivRelevantPds []uint     `json:"-" cborgen:"-"`
}

type ErrorFrame struct {
	Error   string `cborgen:"error"`
	Message string `cborgen:"message"`
}

func (em *EventManager) AddEvent(ctx context.Context, ev *XRPCStreamEvent) error {
	ctx, span := otel.Tracer("events").Start(ctx, "AddEvent")
	defer span.End()

	em.persistAndSendEvent(ctx, ev)
	return nil
}

var ErrPlaybackShutdown = fmt.Errorf("playback shutting down")

func (em *EventManager) Subscribe(ctx context.Context, ident string, filter func(*XRPCStreamEvent) bool, since *int64) (<-chan *XRPCStreamEvent, func(), error) {
	if filter == nil {
		filter = func(*XRPCStreamEvent) bool { return true }
	}

	done := make(chan struct{})
	sub := &Subscriber{
		ident:            ident,
		outgoing:         make(chan *XRPCStreamEvent, em.bufferSize),
		filter:           filter,
		done:             done,
		enqueuedCounter:  eventsEnqueued.WithLabelValues(ident),
		broadcastCounter: eventsBroadcast.WithLabelValues(ident),
	}

	go func() {
		if since != nil {
			if err := em.persister.Playback(ctx, *since, func(e *XRPCStreamEvent) error {
				select {
				case <-done:
					return ErrPlaybackShutdown
				case sub.outgoing <- e:
					return nil
				}
			}); err != nil {
				if errors.Is(err, ErrPlaybackShutdown) {
					log.Warnf("events playback: %s", err)
				} else {
					log.Errorf("events playback: %s", err)
				}
			}
		}

		// if cancel happens before playback completes
		select {
		case <-done:
			return
		default:
		}

		em.addSubscriber(sub)
	}()

	cleanup := func() {
		close(done)
		em.rmSubscriber(sub)
	}

	return sub.outgoing, cleanup, nil
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

func (em *EventManager) HandleRebase(ctx context.Context, user models.Uid) error {
	return em.persister.RebaseRepoEvents(ctx, user)
}
