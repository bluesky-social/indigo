package schemagen

import "fmt"

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
				select {
				case s.outgoing <- op.evt:
				default:
					fmt.Println("event overflow")
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
	EvtKindCreateRecord = "createRecord"
	EvtKindUpdateRecord = "updateRecord"
	EvtKindDeleteRecord = "deleteRecord"
)

type Event struct {
	Kind       string
	User       string
	Collection string
	Rkey       string
	DID        string
	CarSlice   []byte
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
