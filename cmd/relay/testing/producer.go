package testing

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"

	"github.com/bluesky-social/indigo/cmd/relay/stream"

	"github.com/gorilla/websocket"
)

// testing helper which outputs a sequence of events over a websocket
type Producer struct {
	BufferSize int
	mux        *http.ServeMux
	subs       []*Subscriber
	subsLk     sync.Mutex
}

type Subscriber struct {
	outgoing chan *stream.XRPCStreamEvent
	done     chan struct{}
}

func NewProducer() *Producer {
	mux := http.NewServeMux()
	p := Producer{
		BufferSize: 1024,
		mux:        mux,
	}
	mux.HandleFunc("GET /xrpc/com.atproto.sync.subscribeRepos", p.handleSubscribeRepos)
	return &p
}

func (p *Producer) handleSubscribeRepos(resp http.ResponseWriter, req *http.Request) {

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
				slog.Debug("failed to read message from client", "err", err)
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
				slog.Debug("event stream closed unexpectedly")
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
	}
}

func (p *Producer) ListenRandom() int {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		panic(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	slog.Info("starting test producer", "port", port)
	go func() {
		defer listener.Close()
		err := http.Serve(listener, p.mux)
		if err != nil {
			slog.Warn("test producer shutting down", "err", err)
		}
	}()
	return port
}

func (p *Producer) Shutdown() {
	p.subsLk.Lock()
	defer p.subsLk.Unlock()
	for _, sub := range p.subs {
		close(sub.done)
		close(sub.outgoing)
	}
}

func (p *Producer) AddSubscriber(ctx context.Context) (<-chan *stream.XRPCStreamEvent, error) {

	sub := &Subscriber{
		outgoing: make(chan *stream.XRPCStreamEvent, p.BufferSize),
		done:     make(chan struct{}),
	}

	p.subsLk.Lock()
	defer p.subsLk.Unlock()
	p.subs = append(p.subs, sub)

	return sub.outgoing, nil
}

func (p *Producer) Emit(evt *stream.XRPCStreamEvent) error {
	if err := evt.Preserialize(); err != nil {
		return err
	}

	p.subsLk.Lock()
	defer p.subsLk.Unlock()

	if len(p.subs) == 0 {
		slog.Warn("sending event, but no subscribers")
	}
	for _, s := range p.subs {
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
