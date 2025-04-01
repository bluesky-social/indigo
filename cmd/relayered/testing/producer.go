package testing

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/bluesky-social/indigo/events"

	"github.com/gorilla/websocket"
)

// testing helper which outputs a sequence of events over a websocket
type Producer struct {
	Bind       string
	BufferSize int
	mux        *http.ServeMux
	subs       []*Subscriber
	subsLk     sync.Mutex
}

type Subscriber struct {
	outgoing chan *events.XRPCStreamEvent
	done     chan struct{}
}

func NewProducer(bind string) *Producer {
	mux := http.NewServeMux()
	p := Producer{
		Bind:       bind,
		BufferSize: 1024,
		mux:        mux,
	}
	mux.HandleFunc("/xrpc/com.atproto.sync.subscribeRepos", p.handleSubscribeRepos)
	return &p
}

func (p *Producer) handleSubscribeRepos(resp http.ResponseWriter, req *http.Request) {
	slog.Info("XXX: subscribeRepos")

	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()

	conn, err := websocket.Upgrade(resp, req, nil, 1024, 1024)
	if err != nil {
		slog.Error("websocket upgrade", "err", err)
		return
	}

	// read messages from the client and discard them
	go func() {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				slog.Warn("failed to read message from client", "err", err)
				cancel()
				return
			}
		}
	}()

	evts, err := p.AddSubscriber(ctx)
	if err != nil {
		slog.Error("websocket new subscriber", "err", err)
		return
	}

	// pull events from channel and send over websocket
	for {
		select {
		case evt, ok := <-evts:
			if !ok {
				slog.Error("event stream closed unexpectedly")
				return
			}

			wc, err := conn.NextWriter(websocket.BinaryMessage)
			if err != nil {
				slog.Error("failed to get next writer", "err", err)
				return
			}

			if evt.Preserialized != nil {
				_, err = wc.Write(evt.Preserialized)
			} else {
				err = evt.Serialize(wc)
			}
			if err != nil {
				slog.Error("failed to write event", "err", err)
				return
			}

			if err := wc.Close(); err != nil {
				slog.Warn("failed to flush-close our event write", "err", err)
				return
			}
		case <-ctx.Done():
			return
		}
		slog.Info("XXX: emitted event")
	}
}

func (p *Producer) Listen() {
	go func() {
		err := http.ListenAndServe(p.Bind, p.mux)
		if err != nil {
			slog.Error("test producer shutdown", "err", err)
		}
	}()
}

func (p *Producer) Shutdown() {
	p.subsLk.Lock()
	defer p.subsLk.Unlock()
	for _, sub := range p.subs {
		close(sub.done)
		close(sub.outgoing)
	}
}

func (p *Producer) AddSubscriber(ctx context.Context) (<-chan *events.XRPCStreamEvent, error) {

	slog.Info("XXX: adding subscriber")
	sub := &Subscriber{
		outgoing: make(chan *events.XRPCStreamEvent, p.BufferSize),
		done:     make(chan struct{}),
	}

	p.subsLk.Lock()
	defer p.subsLk.Unlock()
	p.subs = append(p.subs, sub)

	return sub.outgoing, nil
}

func (p *Producer) Emit(evt *events.XRPCStreamEvent) error {
	if err := evt.Preserialize(); err != nil {
		return err
	}

	p.subsLk.Lock()
	defer p.subsLk.Unlock()

	if len(p.subs) == 0 {
		slog.Warn("sending event, but no subscribers")
	}
	for _, s := range p.subs {
		slog.Info("XXX: outgoing")
		select {
		case s.outgoing <- evt:
			// sent evt on this subscriber's chan! yay!
		case <-s.done:
			// this subscriber is closing, quickly do nothing
		default:
			return fmt.Errorf("test firehose producer channel blocked")
		}
	}
	return nil
}
