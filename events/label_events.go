package events

import (
	"context"
	"fmt"
)

type LabelEventManager struct {
	subs []*LabelSubscriber

	ops        chan *LabelOperation
	closed     chan struct{}
	bufferSize int

	persister LabelEventPersistence
}

func NewLabelEventManager(persister LabelEventPersistence) *LabelEventManager {
	return &LabelEventManager{
		ops:        make(chan *LabelOperation),
		closed:     make(chan struct{}),
		bufferSize: 1024,
		persister:  persister,
	}
}

type LabelOperation struct {
	op  int
	sub *LabelSubscriber
	evt *LabelStreamEvent
}

func (em *LabelEventManager) Run() {
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
			if err := em.persister.Persist(context.TODO(), op.evt); err != nil {
				log.Errorf("failed to persist outbound event: %s", err)
			}

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
	outgoing chan *LabelStreamEvent

	filter func(*LabelStreamEvent) bool

	done chan struct{}
}

const (
	LEvtKindErrorFrame = -1
	LEvtKindLabelBatch = 1
	LEvtKindInfoFrame  = 2
)

// EventHeader shared with repo events

type LabelStreamEvent struct {
	Batch *LabelBatch
	Info  *InfoFrame
	Error *ErrorFrame
}

type LabelBatch struct {
	Seq    int64   `cborgen:"seq"`
	Labels []Label `cborgen:"labels"`
	// TODO: time?
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

func (em *LabelEventManager) AddEvent(ev *LabelStreamEvent) error {
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

func (em *LabelEventManager) Subscribe(ctx context.Context, filter func(*LabelStreamEvent) bool, since *int64) (<-chan *LabelStreamEvent, func(), error) {
	if filter == nil {
		filter = func(*LabelStreamEvent) bool { return true }
	}

	done := make(chan struct{})
	sub := &LabelSubscriber{
		outgoing: make(chan *LabelStreamEvent, em.bufferSize),
		filter:   filter,
		done:     done,
	}

	go func() {
		if since != nil {
			if err := em.persister.Playback(ctx, *since, func(e *LabelStreamEvent) error {
				select {
				case <-done:
					return fmt.Errorf("shutting down")
				case sub.outgoing <- e:
					return nil
				}
			}); err != nil {
				log.Errorf("events playback: %s", err)
			}
		}

		select {
		case em.ops <- &LabelOperation{
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
		case em.ops <- &LabelOperation{
			op:  opUnsubscribe,
			sub: sub,
		}:
		case <-em.closed:
		}
	}

	return sub.outgoing, cleanup, nil
}
