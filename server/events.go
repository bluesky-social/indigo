package schemagen

import (
	"fmt"
	"log"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
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
					fmt.Println("outgoing event: ", op.evt)
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
	EvtKindCreateRecord = "createRecord"
	EvtKindUpdateRecord = "updateRecord"
	EvtKindDeleteRecord = "deleteRecord"
)

type Event struct {
	Kind string

	// User is the DID of the user this event is about
	User string

	Collection string
	Rkey       string
	DID        string
	CarSlice   []byte

	// some private fields for processing metadata
	uid   uint
	pdsid uint
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

func (s *Server) EventsHandler(c echo.Context) error {
	did := c.Request().Header.Get("DID")
	conn, err := websocket.Upgrade(c.Response().Writer, c.Request(), c.Response().Header(), 1<<10, 1<<10)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()

	var peering Peering
	if err := s.db.First(&peering, "did = ?", did).Error; err != nil {
		return err
	}

	evts, cancel, err := s.events.Subscribe(func(evt *Event) bool {
		has, err := s.peerHasFollow(ctx, peering.ID, evt.uid)
		if err != nil {
			log.Println("error checking peer follow relationship: ", err)
			return false
		}

		fmt.Println("follow: ", has)
		return has
	})
	if err != nil {
		return err
	}
	defer cancel()

	for evt := range evts {
		if err := conn.WriteJSON(evt); err != nil {
			return err
		}
	}

	return nil
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
