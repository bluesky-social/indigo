package events

import (
	"context"
	"errors"
	"fmt"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	label "github.com/bluesky-social/indigo/api/label"

	"github.com/bluesky-social/indigo/util"
	logging "github.com/ipfs/go-log"
	"go.opentelemetry.io/otel"
)

var log = logging.Logger("events")

type EventManager struct {
	subs []*Subscriber

	ops        chan *Operation
	closed     chan struct{}
	bufferSize int

	persister EventPersistence
}

func NewEventManager(persister EventPersistence) *EventManager {
	return &EventManager{
		ops:        make(chan *Operation),
		closed:     make(chan struct{}),
		bufferSize: 1024,
		persister:  persister,
	}
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

func (em *EventManager) Run() {
	for op := range em.ops {
		switch op.op {
		case opSubscribe:
			em.subs = append(em.subs, op.sub)
		case opUnsubscribe:
			for i, s := range em.subs {
				if s == op.sub {
					em.subs[i] = em.subs[len(em.subs)-1]
					em.subs = em.subs[:len(em.subs)-1]
					break
				}
			}
		case opSend:
			fmt.Println("GOT EVENT TO SEND", op.evt.RepoCommit, op.evt.RepoHandle)
			if err := em.persister.Persist(context.TODO(), op.evt); err != nil {
				log.Errorf("failed to persist outbound event: %s", err)
			}

			for _, s := range em.subs {
				if s.filter(op.evt) {
					select {
					case s.outgoing <- op.evt:
						fmt.Println("send event out...")
					case <-s.done:
						go func(torem *Subscriber) {
							select {
							case em.ops <- &Operation{
								op:  opUnsubscribe,
								sub: torem,
							}:
							case <-em.closed:
							}
						}(s)
					default:
						log.Error("event overflow")
					}
				}
			}
		default:
			log.Errorf("unrecognized eventmgr operation: %d", op.op)
		}
	}
}

type Subscriber struct {
	outgoing chan *XRPCStreamEvent

	filter func(*XRPCStreamEvent) bool

	done chan struct{}
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
	PrivUid         util.Uid `json:"-" cborgen:"-"`
	PrivPdsId       uint     `json:"-" cborgen:"-"`
	PrivRelevantPds []uint   `json:"-" cborgen:"-"`
}

type ErrorFrame struct {
	Error   string `cborgen:"error"`
	Message string `cborgen:"message"`
}

func (em *EventManager) AddEvent(ctx context.Context, ev *XRPCStreamEvent) error {
	ctx, span := otel.Tracer("events").Start(ctx, "AddEvent")
	defer span.End()

	select {
	case em.ops <- &Operation{
		op:  opSend,
		evt: ev,
	}:
		return nil
	case <-em.closed:
		return fmt.Errorf("event manager shut down")
	}
}

func (em *EventManager) AddLabelEvent(ev *XRPCStreamEvent) error {
	select {
	case em.ops <- &Operation{
		op:  opSend,
		evt: ev,
	}:
		return nil
	case <-em.closed:
		return fmt.Errorf("event manager shut down")
	}
}

var ErrPlaybackShutdown = fmt.Errorf("playback shutting down")

func (em *EventManager) Subscribe(ctx context.Context, filter func(*XRPCStreamEvent) bool, since *int64) (<-chan *XRPCStreamEvent, func(), error) {
	if filter == nil {
		filter = func(*XRPCStreamEvent) bool { return true }
	}

	done := make(chan struct{})
	sub := &Subscriber{
		outgoing: make(chan *XRPCStreamEvent, em.bufferSize),
		filter:   filter,
		done:     done,
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

		select {
		case em.ops <- &Operation{
			op:  opSubscribe,
			sub: sub,
		}:
		case <-em.closed:
			log.Errorf("failed to subscribe, event manager shut down")
		}
	}()

	cleanup := func() {
		close(done)
		select {
		case em.ops <- &Operation{
			op:  opUnsubscribe,
			sub: sub,
		}:
		case <-em.closed:
		}
	}

	return sub.outgoing, cleanup, nil
}
