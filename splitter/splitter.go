package splitter

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/bgs"
	events "github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/events/schedulers/sequential"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/models"
	"github.com/gorilla/websocket"
	logging "github.com/ipfs/go-log"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	promclient "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
)

var log = logging.Logger("splitter")

type Splitter struct {
	Host   string
	erb    *EventRingBuffer
	events *events.EventManager

	// Management of Socket Consumers
	consumersLk    sync.RWMutex
	nextConsumerID uint64
	consumers      map[uint64]*SocketConsumer
}

func NewSplitter(host string) *Splitter {
	erb := NewEventRingBuffer(20000, 1000)

	em := events.NewEventManager(erb)
	return &Splitter{
		Host:      host,
		erb:       erb,
		events:    em,
		consumers: make(map[uint64]*SocketConsumer),
	}
}

func (s *Splitter) Start(addr string) error {
	var lc net.ListenConfig
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	go s.subscribeWithRedialer(context.Background(), s.Host, 0)

	li, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	return s.StartWithListener(li)
}

func (s *Splitter) StartMetrics(listen string) error {
	http.Handle("/metrics", promhttp.Handler())
	return http.ListenAndServe(listen, nil)
}

func (s *Splitter) Shutdown() error {
	return nil
}

func (s *Splitter) StartWithListener(listen net.Listener) error {
	e := echo.New()
	e.HideBanner = true

	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"http://localhost:*", "https://bgs.bsky-sandbox.dev"},
		AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, echo.HeaderAuthorization},
	}))

	/*
		if !s.ssl {
			e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
				Format: "method=${method}, uri=${uri}, status=${status} latency=${latency_human}\n",
			}))
		} else {
			e.Use(middleware.LoggerWithConfig(middleware.DefaultLoggerConfig))
		}
	*/

	e.Use(bgs.MetricsMiddleware)

	e.HTTPErrorHandler = func(err error, ctx echo.Context) {
		switch err := err.(type) {
		case *echo.HTTPError:
			if err2 := ctx.JSON(err.Code, map[string]any{
				"error": err.Message,
			}); err2 != nil {
				log.Errorf("Failed to write http error: %s", err2)
			}
		default:
			sendHeader := true
			if ctx.Path() == "/xrpc/com.atproto.sync.subscribeRepos" {
				sendHeader = false
			}

			log.Warnf("HANDLER ERROR: (%s) %s", ctx.Path(), err)

			if strings.HasPrefix(ctx.Path(), "/admin/") {
				ctx.JSON(500, map[string]any{
					"error": err.Error(),
				})
				return
			}

			if sendHeader {
				ctx.Response().WriteHeader(500)
			}
		}
	}

	// TODO: this API is temporary until we formalize what we want here

	e.GET("/xrpc/com.atproto.sync.subscribeRepos", s.EventsHandler)
	e.GET("/xrpc/_health", s.HandleHealthCheck)

	// In order to support booting on random ports in tests, we need to tell the
	// Echo instance it's already got a port, and then use its StartServer
	// method to re-use that listener.
	e.Listener = listen
	srv := &http.Server{}
	return e.StartServer(srv)
}

type HealthStatus struct {
	Status  string `json:"status"`
	Message string `json:"msg,omitempty"`
}

func (s *Splitter) HandleHealthCheck(c echo.Context) error {
	return c.JSON(200, HealthStatus{Status: "ok"})
}

func (s *Splitter) EventsHandler(c echo.Context) error {
	var since *int64
	if sinceVal := c.QueryParam("cursor"); sinceVal != "" {
		sval, err := strconv.ParseInt(sinceVal, 10, 64)
		if err != nil {
			return err
		}
		since = &sval
	}

	ctx, cancel := context.WithCancel(c.Request().Context())
	defer cancel()

	// TODO: authhhh
	conn, err := websocket.Upgrade(c.Response(), c.Request(), c.Response().Header(), 10<<10, 10<<10)
	if err != nil {
		return fmt.Errorf("upgrading websocket: %w", err)
	}

	lastWriteLk := sync.Mutex{}
	lastWrite := time.Now()

	// Start a goroutine to ping the client every 30 seconds to check if it's
	// still alive. If the client doesn't respond to a ping within 5 seconds,
	// we'll close the connection and teardown the consumer.
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				lastWriteLk.Lock()
				lw := lastWrite
				lastWriteLk.Unlock()

				if time.Since(lw) < 30*time.Second {
					continue
				}

				if err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(5*time.Second)); err != nil {
					log.Errorf("failed to ping client: %s", err)
					cancel()
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	conn.SetPingHandler(func(message string) error {
		err := conn.WriteControl(websocket.PongMessage, []byte(message), time.Now().Add(time.Second*60))
		if err == websocket.ErrCloseSent {
			return nil
		} else if e, ok := err.(net.Error); ok && e.Temporary() {
			return nil
		}
		return err
	})

	// Start a goroutine to read messages from the client and discard them.
	go func() {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				log.Errorf("failed to read message from client: %s", err)
				cancel()
				return
			}
		}
	}()

	ident := c.RealIP() + "-" + c.Request().UserAgent()

	evts, cleanup, err := s.events.Subscribe(ctx, ident, func(evt *events.XRPCStreamEvent) bool { return true }, since)
	if err != nil {
		return err
	}
	defer cleanup()

	// Keep track of the consumer for metrics and admin endpoints
	consumer := SocketConsumer{
		RemoteAddr:  c.RealIP(),
		UserAgent:   c.Request().UserAgent(),
		ConnectedAt: time.Now(),
	}
	sentCounter := eventsSentCounter.WithLabelValues(consumer.RemoteAddr, consumer.UserAgent)
	consumer.EventsSent = sentCounter

	consumerID := s.registerConsumer(&consumer)
	defer s.cleanupConsumer(consumerID)

	log.Infow("new consumer",
		"remote_addr", consumer.RemoteAddr,
		"user_agent", consumer.UserAgent,
		"cursor", since,
		"consumer_id", consumerID,
	)

	header := events.EventHeader{Op: events.EvtKindMessage}
	for {
		select {
		case evt := <-evts:
			wc, err := conn.NextWriter(websocket.BinaryMessage)
			if err != nil {
				log.Errorf("failed to get next writer: %s", err)
				return err
			}

			var obj lexutil.CBOR

			switch {
			case evt.Error != nil:
				header.Op = events.EvtKindErrorFrame
				obj = evt.Error
			case evt.RepoCommit != nil:
				header.MsgType = "#commit"
				obj = evt.RepoCommit
			case evt.RepoHandle != nil:
				header.MsgType = "#handle"
				obj = evt.RepoHandle
			case evt.RepoInfo != nil:
				header.MsgType = "#info"
				obj = evt.RepoInfo
			case evt.RepoMigrate != nil:
				header.MsgType = "#migrate"
				obj = evt.RepoMigrate
			case evt.RepoTombstone != nil:
				header.MsgType = "#tombstone"
				obj = evt.RepoTombstone
			default:
				return fmt.Errorf("unrecognized event kind")
			}

			if err := header.MarshalCBOR(wc); err != nil {
				return fmt.Errorf("failed to write header: %w", err)
			}

			if err := obj.MarshalCBOR(wc); err != nil {
				return fmt.Errorf("failed to write event: %w", err)
			}

			if err := wc.Close(); err != nil {
				return fmt.Errorf("failed to flush-close our event write: %w", err)
			}
			lastWriteLk.Lock()
			lastWrite = time.Now()
			lastWriteLk.Unlock()
			sentCounter.Inc()
		case <-ctx.Done():
			return nil
		}
	}
}

type SocketConsumer struct {
	UserAgent   string
	RemoteAddr  string
	ConnectedAt time.Time
	EventsSent  promclient.Counter
}

func (s *Splitter) registerConsumer(c *SocketConsumer) uint64 {
	s.consumersLk.Lock()
	defer s.consumersLk.Unlock()

	id := s.nextConsumerID
	s.nextConsumerID++

	s.consumers[id] = c

	return id
}

func (s *Splitter) cleanupConsumer(id uint64) {
	s.consumersLk.Lock()
	defer s.consumersLk.Unlock()

	c := s.consumers[id]

	var m = &dto.Metric{}
	if err := c.EventsSent.Write(m); err != nil {
		log.Errorf("failed to get sent counter: %s", err)
	}

	log.Infow("consumer disconnected",
		"consumer_id", id,
		"remote_addr", c.RemoteAddr,
		"user_agent", c.UserAgent,
		"events_sent", m.Counter.GetValue())

	delete(s.consumers, id)
}

func sleepForBackoff(b int) time.Duration {
	if b == 0 {
		return 0
	}

	if b < 50 {
		return time.Millisecond * time.Duration(rand.Intn(100)+(5*b))
	}

	return time.Second * 5
}

func (s *Splitter) subscribeWithRedialer(ctx context.Context, host string, cursor int64) {
	d := websocket.Dialer{}

	protocol := "wss"

	var backoff int
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		url := fmt.Sprintf("%s://%s/xrpc/com.atproto.sync.subscribeRepos?cursor=%d", protocol, host, cursor)
		con, res, err := d.DialContext(ctx, url, nil)
		if err != nil {
			log.Warnw("dialing failed", "host", host, "err", err, "backoff", backoff)
			time.Sleep(sleepForBackoff(backoff))
			backoff++

			continue
		}

		log.Info("event subscription response code: ", res.StatusCode)

		if err := s.handleConnection(ctx, host, con, &cursor); err != nil {
			log.Warnf("connection to %q failed: %s", host, err)
		}
	}
}

func (s *Splitter) handleConnection(ctx context.Context, host string, con *websocket.Conn, lastCursor *int64) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sched := sequential.NewScheduler("splitter", s.events.AddEvent)
	return events.HandleRepoStream(ctx, con, sched)
}

func sequenceForEvent(evt *events.XRPCStreamEvent) int64 {
	switch {
	case evt.RepoCommit != nil:
		return evt.RepoCommit.Seq
	case evt.RepoHandle != nil:
		return evt.RepoHandle.Seq
	case evt.RepoMigrate != nil:
		return evt.RepoMigrate.Seq
	case evt.RepoTombstone != nil:
		return evt.RepoTombstone.Seq
	case evt.RepoInfo != nil:
		return -1
	default:
		return -1
	}
}

func NewEventRingBuffer(chunkSize, nchunks int) *EventRingBuffer {
	return &EventRingBuffer{
		chunkSize:     chunkSize,
		maxChunkCount: nchunks,
	}
}

type EventRingBuffer struct {
	lk            sync.Mutex
	chunks        []*ringChunk
	chunkSize     int
	maxChunkCount int

	broadcast func(*events.XRPCStreamEvent)
}

type ringChunk struct {
	lk  sync.Mutex
	buf []*events.XRPCStreamEvent
}

func (rc *ringChunk) append(evt *events.XRPCStreamEvent) {
	rc.lk.Lock()
	defer rc.lk.Unlock()
	rc.buf = append(rc.buf, evt)
}

func (rc *ringChunk) events() []*events.XRPCStreamEvent {
	rc.lk.Lock()
	defer rc.lk.Unlock()
	return rc.buf
}

func (er *EventRingBuffer) Persist(ctx context.Context, evt *events.XRPCStreamEvent) error {
	fmt.Println("persist event", sequenceForEvent(evt))
	er.lk.Lock()
	defer er.lk.Unlock()

	if len(er.chunks) == 0 {
		er.chunks = []*ringChunk{new(ringChunk)}
	}

	last := er.chunks[len(er.chunks)-1]
	if len(last.buf) >= er.chunkSize {
		last = new(ringChunk)
		er.chunks = append(er.chunks, last)
		if len(er.chunks) > er.maxChunkCount {
			er.chunks = er.chunks[1:]
		}
	}

	last.append(evt)

	er.broadcast(evt)
	return nil
}

func (er *EventRingBuffer) Flush(context.Context) error {
	return nil
}

func (er *EventRingBuffer) Playback(ctx context.Context, since int64, cb func(*events.XRPCStreamEvent) error) error {
	// grab a snapshot of the current chunks
	er.lk.Lock()
	chunks := er.chunks
	er.lk.Unlock()

	i := len(chunks) - 1
	for ; i >= 0; i-- {
		c := chunks[i]
		evts := c.events()
		if since > sequenceForEvent(evts[len(evts)-1]) {
			i++
			break
		}
	}

	for _, c := range chunks[i:] {
		var nread int
		evts := c.events()
		for nread < len(evts) {
			for _, e := range evts[nread:] {
				if since > 0 && sequenceForEvent(e) < since {
					continue
				}
				since = 0

				if err := cb(e); err != nil {
					return err
				}
			}

			// recheck evts buffer to see if more were added while we were here
			evts = c.events()
		}
	}

	// TODO: probably also check for if new chunks were added while we were iterating...
	return nil
}

func (er *EventRingBuffer) SetEventBroadcaster(brc func(*events.XRPCStreamEvent)) {
	er.broadcast = brc
}

func (er *EventRingBuffer) Shutdown(context.Context) error {
	return nil
}

func (er *EventRingBuffer) TakeDownRepo(context.Context, models.Uid) error {
	return nil
}
