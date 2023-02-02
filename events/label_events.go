package events

import (
	"fmt"
)

type LabelEventManager struct {
	subs []*LabelSubscriber

	ops        chan *LabelOperation
	closed     chan struct{}
	bufferSize int

	persister LabelEventPersistence
}

func NewLabelEventManager() *LabelEventManager {
	return &LabelEventManager{
		ops:        make(chan *LabelOperation),
		closed:     make(chan struct{}),
		bufferSize: 1024,
		persister:  NewMemLabelPersister(),
	}
}

type LabelOperation struct {
	op  int
	sub *LabelSubscriber
	evt *LabelEvent
}

func (em *LabelEventManager) Run() {
	for op := range em.ops {
		switch op.op {
		case opSubscribe:
			em.subs = append(em.subs, op.sub)
			op.sub.outgoing <- &LabelEvent{}
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

type LabelSubscriber struct {
	outgoing chan *LabelEvent

	filter func(*LabelEvent) bool

	done chan struct{}
}

type LabelEvent struct {
	Seq    int64   `cborgen:"seq"`
	Labels []Label `cborgen:"labels"`
}

// this is here, instead of under 'labeling' package, to avoid an import loop
type Label struct {
	LexiconTypeID string  `json:"$type" cborgen:"$type,const=app.bsky.label.label"`
	SourceDid     string  `json:"src" cborgen:"src"`
	SubjectUri    string  `json:"uri" cborgen:"uri"`
	SubjectCid    *string `json:"cid,omitempty" cborgen:"cid"`
	Value         string  `json:"val" cborgen:"val"`
	Timestamp     string  `json:"ts" cborgen:"ts"` // TODO: actual timestamp?
	LabelUri      *string `json:"labeluri,omitempty" cborgen:"labeluri"`
}

func (em *LabelEventManager) AddEvent(ev *LabelEvent) error {
	select {
	case em.ops <- &LabelOperation{
		op:  opSend,
		evt: ev,
	}:
		return nil
	case <-em.closed:
		return fmt.Errorf("event manager shut down")
	}
}

func (em *LabelEventManager) Subscribe(filter func(*LabelEvent) bool, since *int64) (<-chan *LabelEvent, func(), error) {
	if filter == nil {
		filter = func(*LabelEvent) bool { return true }
	}

	done := make(chan struct{})
	sub := &LabelSubscriber{
		outgoing: make(chan *LabelEvent, em.bufferSize),
		filter:   filter,
		done:     done,
	}

	select {
	case em.ops <- &LabelOperation{
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
			if err := em.persister.Playback(*since, func(e *LabelEvent) error {
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
		case em.ops <- &LabelOperation{
			op:  opUnsubscribe,
			sub: sub,
		}:
		case <-em.closed:
		}
	}

	return sub.outgoing, cleanup, nil
}
