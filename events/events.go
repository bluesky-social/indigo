package events

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/models"
	"github.com/prometheus/client_golang/prometheus"

	cbg "github.com/whyrusleeping/cbor-gen"
	"go.opentelemetry.io/otel"
)

var log = slog.Default().With("system", "events")

type Scheduler interface {
	AddWork(ctx context.Context, repo string, val *XRPCStreamEvent) error
	Shutdown()
}

type EventManager struct {
	subs   []*Subscriber
	subsLk sync.Mutex

	bufferSize          int
	crossoverBufferSize int

	persister EventPersistence

	log *slog.Logger
}

func NewEventManager(persister EventPersistence) *EventManager {
	em := &EventManager{
		bufferSize:          16 << 10,
		crossoverBufferSize: 512,
		persister:           persister,
		log:                 slog.Default().With("system", "events"),
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
				s.filter = func(*XRPCStreamEvent) bool { return false }

				em.log.Warn("dropping slow consumer due to event overflow", "bufferSize", len(s.outgoing), "ident", s.ident)
				go func(torem *Subscriber) {
					torem.lk.Lock()
					if !torem.cleanedUp {
						select {
						case torem.outgoing <- &XRPCStreamEvent{
							Error: &ErrorFrame{
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

func (em *EventManager) persistAndSendEvent(ctx context.Context, evt *XRPCStreamEvent) {
	// TODO: can cut 5-10% off of disk persister benchmarks by making this function
	// accept a uid. The lookup inside the persister is notably expensive (despite
	// being an lru cache?)
	if err := em.persister.Persist(ctx, evt); err != nil {
		em.log.Error("failed to persist outbound event", "err", err)
	}
}

type Subscriber struct {
	outgoing chan *XRPCStreamEvent

	filter func(*XRPCStreamEvent) bool

	done chan struct{}

	cleanup func()

	lk        sync.Mutex
	cleanedUp bool

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

var (
	AccountStatusActive      = "active"
	AccountStatusTakendown   = "takendown"
	AccountStatusSuspended   = "suspended"
	AccountStatusDeleted     = "deleted"
	AccountStatusDeactivated = "deactivated"
)

type XRPCStreamEvent struct {
	Error         *ErrorFrame
	RepoCommit    *comatproto.SyncSubscribeRepos_Commit
	RepoHandle    *comatproto.SyncSubscribeRepos_Handle
	RepoIdentity  *comatproto.SyncSubscribeRepos_Identity
	RepoInfo      *comatproto.SyncSubscribeRepos_Info
	RepoMigrate   *comatproto.SyncSubscribeRepos_Migrate
	RepoTombstone *comatproto.SyncSubscribeRepos_Tombstone
	RepoAccount   *comatproto.SyncSubscribeRepos_Account
	LabelLabels   *comatproto.LabelSubscribeLabels_Labels
	LabelInfo     *comatproto.LabelSubscribeLabels_Info

	// some private fields for internal routing perf
	PrivUid         models.Uid `json:"-" cborgen:"-"`
	PrivPdsId       uint       `json:"-" cborgen:"-"`
	PrivRelevantPds []uint     `json:"-" cborgen:"-"`
	Preserialized   []byte     `json:"-" cborgen:"-"`
}

func (evt *XRPCStreamEvent) Serialize(wc io.Writer) error {
	header := EventHeader{Op: EvtKindMessage}
	var obj lexutil.CBOR

	switch {
	case evt.Error != nil:
		header.Op = EvtKindErrorFrame
		obj = evt.Error
	case evt.RepoCommit != nil:
		header.MsgType = "#commit"
		obj = evt.RepoCommit
	case evt.RepoHandle != nil:
		header.MsgType = "#handle"
		obj = evt.RepoHandle
	case evt.RepoIdentity != nil:
		header.MsgType = "#identity"
		obj = evt.RepoIdentity
	case evt.RepoAccount != nil:
		header.MsgType = "#account"
		obj = evt.RepoAccount
	case evt.RepoInfo != nil:
		header.MsgType = "#info"
		obj = evt.RepoInfo
	case evt.RepoMigrate != nil:
		header.MsgType = "#migrate"
		obj = evt.RepoMigrate
	case evt.RepoTombstone != nil:
		header.MsgType = "#tombstone"
		obj = evt.RepoTombstone
	default:
		return fmt.Errorf("unrecognized event kind")
	}

	cborWriter := cbg.NewCborWriter(wc)
	if err := header.MarshalCBOR(cborWriter); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}
	return obj.MarshalCBOR(cborWriter)
}

func (xevt *XRPCStreamEvent) Deserialize(r io.Reader) error {
	var header EventHeader
	if err := header.UnmarshalCBOR(r); err != nil {
		return fmt.Errorf("reading header: %w", err)
	}
	switch header.Op {
	case EvtKindMessage:
		switch header.MsgType {
		case "#commit":
			var evt comatproto.SyncSubscribeRepos_Commit
			if err := evt.UnmarshalCBOR(r); err != nil {
				return fmt.Errorf("reading repoCommit event: %w", err)
			}
			xevt.RepoCommit = &evt
		case "#handle":
			var evt comatproto.SyncSubscribeRepos_Handle
			if err := evt.UnmarshalCBOR(r); err != nil {
				return err
			}
			xevt.RepoHandle = &evt
		case "#identity":
			var evt comatproto.SyncSubscribeRepos_Identity
			if err := evt.UnmarshalCBOR(r); err != nil {
				return err
			}
			xevt.RepoIdentity = &evt
		case "#account":
			var evt comatproto.SyncSubscribeRepos_Account
			if err := evt.UnmarshalCBOR(r); err != nil {
				return err
			}
			xevt.RepoAccount = &evt
		case "#info":
			// TODO: this might also be a LabelInfo (as opposed to RepoInfo)
			var evt comatproto.SyncSubscribeRepos_Info
			if err := evt.UnmarshalCBOR(r); err != nil {
				return err
			}
			xevt.RepoInfo = &evt
		case "#migrate":
			var evt comatproto.SyncSubscribeRepos_Migrate
			if err := evt.UnmarshalCBOR(r); err != nil {
				return err
			}
			xevt.RepoMigrate = &evt
		case "#tombstone":
			var evt comatproto.SyncSubscribeRepos_Tombstone
			if err := evt.UnmarshalCBOR(r); err != nil {
				return err
			}
			xevt.RepoTombstone = &evt
		case "#labels":
			var evt comatproto.LabelSubscribeLabels_Labels
			if err := evt.UnmarshalCBOR(r); err != nil {
				return fmt.Errorf("reading Labels event: %w", err)
			}
			xevt.LabelLabels = &evt
		}
	case EvtKindErrorFrame:
		var errframe ErrorFrame
		if err := errframe.UnmarshalCBOR(r); err != nil {
			return err
		}
		xevt.Error = &errframe
	default:
		return fmt.Errorf("unrecognized event stream type: %d", header.Op)
	}
	return nil
}

var ErrNoSeq = errors.New("event has no sequence number")

// serialize content into Preserialized cache
func (evt *XRPCStreamEvent) Preserialize() error {
	if evt.Preserialized != nil {
		return nil
	}
	var buf bytes.Buffer
	err := evt.Serialize(&buf)
	if err != nil {
		return err
	}
	evt.Preserialized = buf.Bytes()
	return nil
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

var (
	ErrPlaybackShutdown = fmt.Errorf("playback shutting down")
	ErrCaughtUp         = fmt.Errorf("caught up")
)

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

	out := make(chan *XRPCStreamEvent, em.crossoverBufferSize)

	go func() {
		lastSeq := *since
		// run playback to get through *most* of the events, getting our current cursor close to realtime
		if err := em.persister.Playback(ctx, *since, func(e *XRPCStreamEvent) error {
			select {
			case <-done:
				return ErrPlaybackShutdown
			case out <- e:
				seq := SequenceForEvent(e)
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
		if err := em.persister.Playback(ctx, lastSeq, func(e *XRPCStreamEvent) error {
			seq := SequenceForEvent(e)
			if seq > SequenceForEvent(first) {
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

func SequenceForEvent(evt *XRPCStreamEvent) int64 {
	return evt.Sequence()
}

func (evt *XRPCStreamEvent) Sequence() int64 {
	switch {
	case evt == nil:
		return -1
	case evt.RepoCommit != nil:
		return evt.RepoCommit.Seq
	case evt.RepoHandle != nil:
		return evt.RepoHandle.Seq
	case evt.RepoMigrate != nil:
		return evt.RepoMigrate.Seq
	case evt.RepoTombstone != nil:
		return evt.RepoTombstone.Seq
	case evt.RepoIdentity != nil:
		return evt.RepoIdentity.Seq
	case evt.RepoAccount != nil:
		return evt.RepoAccount.Seq
	case evt.RepoInfo != nil:
		return -1
	case evt.Error != nil:
		return -1
	default:
		return -1
	}
}

func (evt *XRPCStreamEvent) GetSequence() (int64, bool) {
	switch {
	case evt == nil:
		return -1, false
	case evt.RepoCommit != nil:
		return evt.RepoCommit.Seq, true
	case evt.RepoHandle != nil:
		return evt.RepoHandle.Seq, true
	case evt.RepoMigrate != nil:
		return evt.RepoMigrate.Seq, true
	case evt.RepoTombstone != nil:
		return evt.RepoTombstone.Seq, true
	case evt.RepoIdentity != nil:
		return evt.RepoIdentity.Seq, true
	case evt.RepoAccount != nil:
		return evt.RepoAccount.Seq, true
	case evt.RepoInfo != nil:
		return -1, false
	case evt.Error != nil:
		return -1, false
	default:
		return -1, false
	}
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
