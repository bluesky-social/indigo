package events

import (
	"fmt"

	cid "github.com/ipfs/go-cid"
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
	evt *RepoEvent
}

func (em *EventManager) Run() {
	for op := range em.ops {
		switch op.op {
		case opSubscribe:
			em.subs = append(em.subs, op.sub)
			op.sub.outgoing <- &RepoEvent{}
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
	outgoing chan *RepoEvent

	filter func(*RepoEvent) bool

	done chan struct{}
}

const (
	EvtKindRepoChange = "repoChange"
)

type EventHeader struct {
	Type string `cborgen:"t"`
}

type RepoEvent struct {
	Seq int64 `cborgen:"seq"`

	// Repo is the DID of the repo this event is about
	Repo string `cborgen:"repo"`

	RepoAppend *RepoAppend `cborgen:"repoAppend,omitempty"`

	// some private fields for internal routing perf
	PrivUid         uint   `json:"-" cborgen:"-"`
	PrivPdsId       uint   `json:"-" cborgen:"-"`
	PrivRelevantPds []uint `json:"-" cborgen:"-"`
}

type RepoAppend struct {
	Prev   *cid.Cid  `cborgen:"prev"`
	Ops    []*RepoOp `cborgen:"ops"`
	Rebase bool      `cborgen:"rebase"`
	Car    []byte    `cborgen:"car"`
}

type RepoOp struct {
	Kind string `cborgen:"kind"`
	Col  string `cborgen:"col"`
	Rkey string `cborgen:"rkey"`
}

func (em *EventManager) AddEvent(ev *RepoEvent) error {
	if ev.RepoAppend != nil {
		for _, op := range ev.RepoAppend.Ops {
			if op == nil {
				panic("no nil things pls")
			}
		}
	}
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

func (em *EventManager) Subscribe(filter func(*RepoEvent) bool, since *int64) (<-chan *RepoEvent, func(), error) {
	if filter == nil {
		filter = func(*RepoEvent) bool { return true }
	}

	done := make(chan struct{})
	sub := &Subscriber{
		outgoing: make(chan *RepoEvent, em.bufferSize),
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
			if err := em.persister.Playback(*since, func(e *RepoEvent) error {
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
