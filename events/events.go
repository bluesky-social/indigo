package events

import (
	"fmt"
)

type EventManager struct {
	subs []*Subscriber

	ops        chan *Operation
	closed     chan struct{}
	bufferSize int
}

func NewEventManager() *EventManager {
	return &EventManager{
		ops:        make(chan *Operation),
		closed:     make(chan struct{}),
		bufferSize: 1024,
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
	evt *Event
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
			for _, s := range em.subs {
				if s.filter(op.evt) {
					select {
					case s.outgoing <- op.evt:
					default:
						fmt.Println("event overflow")
					}
				}
			}
		default:
			fmt.Printf("unrecognized eventmgr operation: %d\n", op.op)
		}
	}
}

type Subscriber struct {
	outgoing chan *Event

	filter func(*Event) bool
}

const (
	EvtKindRepoChange = "repoChange"
)

type Event struct {

	// Repo is the DID of the repo this event is about
	Repo string

	Kind string

	RepoOps    []*RepoOp
	RepoRebase bool
	CarSlice   []byte

	// some private fields for internal routing perf
	PrivUid         uint   `json:"-"`
	PrivPdsId       uint   `json:"-"`
	PrivRelevantPds []uint `json:"-"`
}

type RepoOp struct {
	Kind       string
	Collection string
	Rkey       string

	PrivRelevantPds []uint `json:"-"`
}

func (em *EventManager) AddEvent(ev *Event) error {
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

func (em *EventManager) Subscribe(filter func(*Event) bool) (<-chan *Event, func(), error) {
	if filter == nil {
		filter = func(*Event) bool { return true }
	}
	sub := &Subscriber{
		outgoing: make(chan *Event, em.bufferSize),
		filter:   filter,
	}

	select {
	case em.ops <- &Operation{
		op:  opSubscribe,
		sub: sub,
	}:
	case <-em.closed:
		return nil, nil, fmt.Errorf("event manager shut down")
	}

	cleanup := func() {
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
