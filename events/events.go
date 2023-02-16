package events

import (
	"fmt"

	logging "github.com/ipfs/go-log"
)

var log = logging.Logger("events")

type EventManager struct {
	subs []*Subscriber

	ops        chan *Operation
	closed     chan struct{}
	bufferSize int

	persister EventPersistence
}

func NewEventManager() *EventManager {
	return &EventManager{
		ops:        make(chan *Operation),
		closed:     make(chan struct{}),
		bufferSize: 1024,
		persister:  NewMemPersister(),
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
	evt *RepoStreamEvent
}

func (em *EventManager) Run() {
	for op := range em.ops {
		switch op.op {
		case opSubscribe:
			em.subs = append(em.subs, op.sub)
			op.sub.outgoing <- &RepoStreamEvent{}
		case opUnsubscribe:
			for i, s := range em.subs {
				if s == op.sub {
					em.subs[i] = em.subs[len(em.subs)-1]
					em.subs = em.subs[:len(em.subs)-1]
					break
				}
			}
		case opSend:
			em.persister.Persist(op.evt)

			for _, s := range em.subs {
				if s.filter(op.evt) {
					select {
					case s.outgoing <- op.evt:
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
	outgoing chan *RepoStreamEvent

	filter func(*RepoStreamEvent) bool

	done chan struct{}
}

const (
	EvtKindRepoAppend = 0
	EvtKindRepoRebase = 1
)

type EventHeader struct {
	Type int64 `cborgen:"t"`
}

type RepoStreamEvent struct {
	Append *RepoAppend

	// some private fields for internal routing perf
	PrivUid         uint   `json:"-" cborgen:"-"`
	PrivPdsId       uint   `json:"-" cborgen:"-"`
	PrivRelevantPds []uint `json:"-" cborgen:"-"`
}

type RepoAppend struct {
	Seq int64 `cborgen:"seq"`

	// Repo is the DID of the repo this event is about
	Repo string `cborgen:"repo"`

	Commit string `cborgen:"commit"`
	Prev   string `cborgen:"prev"`
	//Commit cid.Cid  `cborgen:"commit"`
	//Prev   *cid.Cid `cborgen:"prev"`

	Blocks []byte `cborgen:"blocks"`
}

func (em *EventManager) AddEvent(ev *RepoStreamEvent) error {
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

func (em *EventManager) Subscribe(filter func(*RepoStreamEvent) bool, since *int64) (<-chan *RepoStreamEvent, func(), error) {
	if filter == nil {
		filter = func(*RepoStreamEvent) bool { return true }
	}

	done := make(chan struct{})
	sub := &Subscriber{
		outgoing: make(chan *RepoStreamEvent, em.bufferSize),
		filter:   filter,
		done:     done,
	}

	select {
	case em.ops <- &Operation{
		op:  opSubscribe,
		sub: sub,
	}:
	case <-em.closed:
		return nil, nil, fmt.Errorf("event manager shut down")
	}

	// receive the 'ack' that ensures our sub was received
	<-sub.outgoing

	if since != nil {
		go func() {
			if err := em.persister.Playback(*since, func(e *RepoStreamEvent) error {
				select {
				case <-done:
					return fmt.Errorf("shutting down")
				case sub.outgoing <- e:
					return nil
				}
			}); err != nil {
				log.Errorf("events playback: %s", err)
			}
		}()
	}

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
