package splitter

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/bgs"
	events "github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/events/schedulers/sequential"
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
	pp     *events.PebblePersist
	events *events.EventManager

	// cursor storage
	cursorFile string

	// Management of Socket Consumers
	consumersLk    sync.RWMutex
	nextConsumerID uint64
	consumers      map[uint64]*SocketConsumer
}

func NewMemSplitter(host string) *Splitter {
	erb := NewEventRingBuffer(20_000, 10_000)

	em := events.NewEventManager(erb)
	return &Splitter{
		cursorFile: "cursor-file",
		Host:       host,
		erb:        erb,
		events:     em,
		consumers:  make(map[uint64]*SocketConsumer),
	}
}
func NewDiskSplitter(host, path string, persistHours float64) (*Splitter, error) {
	pp, err := events.NewPebblePersistance(path)
	if err != nil {
		return nil, err
	}

	go pp.GCThread(context.Background(), time.Duration(float64(time.Hour)*persistHours), 5*time.Minute)
	em := events.NewEventManager(pp)
	return &Splitter{
		cursorFile: "cursor-file",
		Host:       host,
		pp:         pp,
		events:     em,
		consumers:  make(map[uint64]*SocketConsumer),
	}, nil
}

func (s *Splitter) Start(addr string) error {
	var lc net.ListenConfig
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	curs, err := s.getLastCursor()
	if err != nil {
		return fmt.Errorf("loading cursor failed: %w", err)
	}

	go s.subscribeWithRedialer(context.Background(), s.Host, curs)

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
	activeClientGauge.Inc()
	defer activeClientGauge.Dec()

	for {
		select {
		case evt, ok := <-evts:
			if !ok {
				log.Error("event stream closed unexpectedly")
				return nil
			}

			wc, err := conn.NextWriter(websocket.BinaryMessage)
			if err != nil {
				log.Errorf("failed to get next writer: %s", err)
				return err
			}

			if evt.Preserialized != nil {
				_, err = wc.Write(evt.Preserialized)
			} else {
				err = evt.Serialize(wc)
			}
			if err != nil {
				return fmt.Errorf("failed to write event: %w", err)
			}

			if err := wc.Close(); err != nil {
				log.Warnf("failed to flush-close our event write: %s", err)
				return nil
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

		header := http.Header{
			"User-Agent": []string{"bgs-rainbow-v0"},
		}

		url := fmt.Sprintf("%s://%s/xrpc/com.atproto.sync.subscribeRepos?cursor=%d", protocol, host, cursor)
		con, res, err := d.DialContext(ctx, url, header)
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

	sched := sequential.NewScheduler("splitter", func(ctx context.Context, evt *events.XRPCStreamEvent) error {
		seq := events.SequenceForEvent(evt)
		if seq < 0 {
			// ignore info events and other unsupported types
			return nil
		}

		if err := s.events.AddEvent(ctx, evt); err != nil {
			return err
		}

		if seq%5000 == 0 {
			if err := s.writeCursor(seq); err != nil {
				log.Errorf("write cursor failed: %s", err)
			}
		}

		*lastCursor = seq
		return nil
	})

	return events.HandleRepoStream(ctx, con, sched)
}

func (s *Splitter) getLastCursor() (int64, error) {
	fi, err := os.Open(s.cursorFile)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	b, err := io.ReadAll(fi)
	if err != nil {
		return 0, err
	}

	v, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil {
		return 0, err
	}

	return v, nil
}

func (s *Splitter) writeCursor(curs int64) error {
	return os.WriteFile(s.cursorFile, []byte(fmt.Sprint(curs)), 0664)
}
