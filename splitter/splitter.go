package splitter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go.opentelemetry.io/otel"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/bgs"
	events "github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/events/schedulers/sequential"
	"github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	promclient "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
)

type Splitter struct {
	erb    *EventRingBuffer
	pp     *events.PebblePersist
	events *events.EventManager

	// Management of Socket Consumers
	consumersLk    sync.RWMutex
	nextConsumerID uint64
	consumers      map[uint64]*SocketConsumer

	conf SplitterConfig

	log *slog.Logger

	httpC        *http.Client
	nextCrawlers []*url.URL
}

type SplitterConfig struct {
	UpstreamHost  string
	CursorFile    string
	PebbleOptions *events.PebblePersistOptions
}

func (sc *SplitterConfig) XrpcRootUrl() string {
	if strings.HasPrefix(sc.UpstreamHost, "http://") {
		return sc.UpstreamHost
	}
	if strings.HasPrefix(sc.UpstreamHost, "https://") {
		return sc.UpstreamHost
	}
	if strings.HasPrefix(sc.UpstreamHost, "ws://") {
		return "http://" + sc.UpstreamHost[5:]
	}
	if strings.HasPrefix(sc.UpstreamHost, "wss://") {
		return "https://" + sc.UpstreamHost[6:]
	}
	return "https://" + sc.UpstreamHost
}

func NewSplitter(conf SplitterConfig, nextCrawlers []string) (*Splitter, error) {
	var nextCrawlerURLs []*url.URL
	log := slog.Default().With("system", "splitter")
	if len(nextCrawlers) > 0 {
		nextCrawlerURLs = make([]*url.URL, len(nextCrawlers))
		for i, tu := range nextCrawlers {
			var err error
			nextCrawlerURLs[i], err = url.Parse(tu)
			if err != nil {
				return nil, fmt.Errorf("failed to parse next-crawler url: %w", err)
			}
			log.Info("configuring relay for requestCrawl", "host", nextCrawlerURLs[i])
		}
	}

	s := &Splitter{
		conf:         conf,
		consumers:    make(map[uint64]*SocketConsumer),
		log:          log,
		httpC:        util.RobustHTTPClient(),
		nextCrawlers: nextCrawlerURLs,
	}

	if conf.PebbleOptions == nil {
		// mem splitter
		erb := NewEventRingBuffer(20_000, 10_000)
		s.erb = erb
		s.events = events.NewEventManager(erb)
	} else {
		pp, err := events.NewPebblePersistance(conf.PebbleOptions)
		if err != nil {
			return nil, err
		}
		go pp.GCThread(context.Background())
		s.pp = pp
		s.events = events.NewEventManager(pp)
	}

	return s, nil
}
func NewDiskSplitter(host, path string, persistHours float64, maxBytes int64) (*Splitter, error) {
	ppopts := events.PebblePersistOptions{
		DbPath:          path,
		PersistDuration: time.Duration(float64(time.Hour) * persistHours),
		GCPeriod:        5 * time.Minute,
		MaxBytes:        uint64(maxBytes),
	}
	conf := SplitterConfig{
		UpstreamHost:  host,
		CursorFile:    "cursor-file",
		PebbleOptions: &ppopts,
	}
	pp, err := events.NewPebblePersistance(&ppopts)
	if err != nil {
		return nil, err
	}

	go pp.GCThread(context.Background())
	em := events.NewEventManager(pp)
	return &Splitter{
		conf:      conf,
		pp:        pp,
		events:    em,
		consumers: make(map[uint64]*SocketConsumer),
		log:       slog.Default().With("system", "splitter"),
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

	go s.subscribeWithRedialer(context.Background(), s.conf.UpstreamHost, curs)

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
		AllowOrigins: []string{"*"},
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
				s.log.Error("Failed to write http error", "err", err2)
			}
		default:
			sendHeader := true
			if ctx.Path() == "/xrpc/com.atproto.sync.subscribeRepos" {
				sendHeader = false
			}

			s.log.Warn("HANDLER ERROR", "path", ctx.Path(), "err", err)

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

	e.POST("/xrpc/com.atproto.sync.requestCrawl", s.RequestCrawlHandler)
	e.GET("/xrpc/com.atproto.sync.subscribeRepos", s.EventsHandler)
	e.GET("/xrpc/com.atproto.sync.listRepos", s.HandleComAtprotoSyncListRepos)

	e.GET("/xrpc/_health", s.HandleHealthCheck)
	e.GET("/_health", s.HandleHealthCheck)
	e.GET("/", s.HandleHomeMessage)

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

var homeMessage string = `
          _      _
 _ _ __ _(_)_ _ | |__  _____ __ __
| '_/ _' | | ' \| '_ \/ _ \ V  V /
|_| \__,_|_|_||_|_.__/\___/\_/\_/

This is an atproto [https://atproto.com] firehose fanout service, running the 'rainbow' codebase [https://github.com/bluesky-social/indigo]

The firehose WebSocket path is at:  /xrpc/com.atproto.sync.subscribeRepos
`

func (s *Splitter) HandleHomeMessage(c echo.Context) error {
	return c.String(http.StatusOK, homeMessage)
}

type XRPCError struct {
	Message string `json:"message"`
}

func (s *Splitter) RequestCrawlHandler(c echo.Context) error {
	ctx := c.Request().Context()
	var body comatproto.SyncRequestCrawl_Input
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, XRPCError{Message: fmt.Sprintf("invalid body: %s", err)})
	}

	host := body.Hostname
	if host == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "must pass hostname")
	}

	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "https://" + host
	}

	u, err := url.Parse(host)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to parse hostname")
	}

	if u.Scheme == "http" {
		return echo.NewHTTPError(http.StatusBadRequest, "this server requires https")
	}
	if u.Path != "" {
		return echo.NewHTTPError(http.StatusBadRequest, "must pass hostname without path")
	}

	if u.Query().Encode() != "" {
		return echo.NewHTTPError(http.StatusBadRequest, "must pass hostname without query")
	}

	host = u.Host // potentially hostname:port

	clientHost := fmt.Sprintf("%s://%s", u.Scheme, host)

	xrpcC := &xrpc.Client{
		Host:   clientHost,
		Client: http.DefaultClient, // not using the client that auto-retries
	}

	desc, err := atproto.ServerDescribeServer(ctx, xrpcC)
	if err != nil {
		errMsg := fmt.Sprintf("requested host (%s) failed to respond to describe request", clientHost)
		return echo.NewHTTPError(http.StatusBadRequest, errMsg)
	}

	// Maybe we could do something with this response later
	_ = desc

	if len(s.nextCrawlers) != 0 {
		blob, err := json.Marshal(body)
		if err != nil {
			s.log.Warn("could not forward requestCrawl, json err", "err", err)
		} else {
			go func(bodyBlob []byte) {
				for _, remote := range s.nextCrawlers {
					if remote == nil {
						continue
					}

					pu := remote.JoinPath("/xrpc/com.atproto.sync.requestCrawl")
					response, err := s.httpC.Post(pu.String(), "application/json", bytes.NewReader(bodyBlob))
					if response != nil && response.Body != nil {
						response.Body.Close()
					}
					if err != nil || response == nil {
						s.log.Warn("requestCrawl forward failed", "host", remote, "err", err)
					} else if response.StatusCode != http.StatusOK {
						s.log.Warn("requestCrawl forward failed", "host", remote, "status", response.Status)
					} else {
						s.log.Info("requestCrawl forward successful", "host", remote)
					}
				}
			}(blob)
		}
	}

	return c.JSON(200, HealthStatus{Status: "ok"})
}

func (s *Splitter) HandleComAtprotoSyncListRepos(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncListRepos")
	defer span.End()

	cursorQuery := c.QueryParam("cursor")
	limitQuery := c.QueryParam("limit")

	var err error

	limit := int64(500)
	if limitQuery != "" {
		limit, err = strconv.ParseInt(limitQuery, 10, 64)
		if err != nil || limit < 1 || limit > 1000 {
			return c.JSON(http.StatusBadRequest, XRPCError{Message: fmt.Sprintf("invalid limit: %s", limitQuery)})
		}
	}

	client := xrpc.Client{
		Client: s.httpC,
		Host:   s.conf.XrpcRootUrl(),
	}

	out, handleErr := atproto.SyncListRepos(ctx, &client, cursorQuery, limit)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
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
					s.log.Error("failed to ping client", "err", err)
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
				s.log.Error("failed to read message from client", "err", err)
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

	s.log.Info("new consumer",
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
				s.log.Error("event stream closed unexpectedly")
				return nil
			}

			wc, err := conn.NextWriter(websocket.BinaryMessage)
			if err != nil {
				s.log.Error("failed to get next writer", "err", err)
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
				s.log.Warn("failed to flush-close our event write", "err", err)
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
		s.log.Error("failed to get sent counter", "err", err)
	}

	s.log.Info("consumer disconnected",
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

		var url string
		if cursor < 0 {
			url = fmt.Sprintf("%s://%s/xrpc/com.atproto.sync.subscribeRepos", protocol, host)
		} else {
			url = fmt.Sprintf("%s://%s/xrpc/com.atproto.sync.subscribeRepos?cursor=%d", protocol, host, cursor)
		}
		con, res, err := d.DialContext(ctx, url, header)
		if err != nil {
			s.log.Warn("dialing failed", "host", host, "err", err, "backoff", backoff)
			time.Sleep(sleepForBackoff(backoff))
			backoff++

			continue
		}

		s.log.Info("event subscription response", "code", res.StatusCode)

		if err := s.handleConnection(ctx, host, con, &cursor); err != nil {
			s.log.Warn("connection failed", "host", host, "err", err)
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
			// TODO: don't need this after we move to getting seq from pebble
			if err := s.writeCursor(seq); err != nil {
				s.log.Error("write cursor failed", "err", err)
			}
		}

		*lastCursor = seq
		return nil
	})

	return events.HandleRepoStream(ctx, con, sched, nil)
}

func (s *Splitter) getLastCursor() (int64, error) {
	if s.pp != nil {
		seq, millis, _, err := s.pp.GetLast(context.Background())
		if err == nil {
			s.log.Debug("got last cursor from pebble", "seq", seq, "millis", millis)
			return seq, nil
		} else if errors.Is(err, events.ErrNoLast) {
			s.log.Info("pebble no last")
		} else {
			s.log.Error("pebble seq fail", "err", err)
		}
	}

	fi, err := os.Open(s.conf.CursorFile)
	if err != nil {
		if os.IsNotExist(err) {
			return -1, nil
		}
		return -1, err
	}

	b, err := io.ReadAll(fi)
	if err != nil {
		return -1, err
	}

	v, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil {
		return -1, err
	}

	return v, nil
}

func (s *Splitter) writeCursor(curs int64) error {
	return os.WriteFile(s.conf.CursorFile, []byte(fmt.Sprint(curs)), 0664)
}
