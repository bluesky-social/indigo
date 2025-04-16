package splitter

import (
	"context"
	"errors"
	"fmt"
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

	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/events/pebblepersist"
	"github.com/bluesky-social/indigo/events/schedulers/sequential"
	"github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/util/svcutil"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Splitter struct {
	erb    *EventRingBuffer
	pp     *pebblepersist.PebblePersist
	events *events.EventManager

	// Management of Socket Consumers
	consumersLk    sync.RWMutex
	nextConsumerID uint64
	consumers      map[uint64]*SocketConsumer

	conf SplitterConfig

	logger *slog.Logger

	upstreamClient *http.Client
	nextCrawlers   []*url.URL
}

type SplitterConfig struct {
	UpstreamHost      string
	CollectionDirHost string
	CursorFile        string
	UserAgent         string
	PebbleOptions     *pebblepersist.PebblePersistOptions
	Logger            *slog.Logger
}

func (sc *SplitterConfig) UpstreamHostWebsocket() string {

	if !strings.Contains(sc.UpstreamHost, "://") {
		return "wss://" + sc.UpstreamHost
	}
	u, err := url.Parse(sc.UpstreamHost)
	if err != nil {
		// this will cause an error downstream
		return ""
	}

	switch u.Scheme {
	case "http", "ws":
		return "ws://" + u.Host
	case "https", "wss":
		return "wss://" + u.Host
	default:
		return "wss://" + u.Host
	}
}

func (sc *SplitterConfig) UpstreamHostHTTP() string {

	if !strings.Contains(sc.UpstreamHost, "://") {
		return "https://" + sc.UpstreamHost
	}
	u, err := url.Parse(sc.UpstreamHost)
	if err != nil {
		// this will cause an error downstream
		return ""
	}

	switch u.Scheme {
	case "http", "ws":
		return "http://" + u.Host
	case "https", "wss":
		return "https://" + u.Host
	default:
		return "https://" + u.Host
	}
}

func NewSplitter(conf SplitterConfig, nextCrawlers []string) (*Splitter, error) {

	logger := conf.Logger
	if logger == nil {
		logger = slog.Default().With("system", "splitter")
	}

	var nextCrawlerURLs []*url.URL
	if len(nextCrawlers) > 0 {
		nextCrawlerURLs = make([]*url.URL, len(nextCrawlers))
		for i, tu := range nextCrawlers {
			var err error
			nextCrawlerURLs[i], err = url.Parse(tu)
			if err != nil {
				return nil, fmt.Errorf("failed to parse next-crawler url: %w", err)
			}
			logger.Info("configuring relay for requestCrawl", "host", nextCrawlerURLs[i])
		}
	}

	_, err := url.Parse(conf.UpstreamHostHTTP())
	if err != nil {
		return nil, fmt.Errorf("failed to parse upstream url %#v: %w", conf.UpstreamHostHTTP(), err)
	}

	s := &Splitter{
		conf:           conf,
		consumers:      make(map[uint64]*SocketConsumer),
		logger:         logger,
		upstreamClient: util.RobustHTTPClient(),
		nextCrawlers:   nextCrawlerURLs,
	}

	if conf.PebbleOptions == nil {
		// mem splitter
		erb := NewEventRingBuffer(20_000, 10_000)
		s.erb = erb
		s.events = events.NewEventManager(erb)
	} else {
		pp, err := pebblepersist.NewPebblePersistance(conf.PebbleOptions)
		if err != nil {
			return nil, err
		}
		go pp.GCThread(context.Background())
		s.pp = pp
		s.events = events.NewEventManager(pp)
	}

	return s, nil
}

func (s *Splitter) StartAPI(addr string) error {
	var lc net.ListenConfig
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	curs, err := s.getLastCursor()
	if err != nil {
		return fmt.Errorf("loading cursor failed: %w", err)
	}

	go s.subscribeWithRedialer(context.Background(), curs)

	li, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	return s.startWithListener(li)
}

func (s *Splitter) StartMetrics(listen string) error {
	http.Handle("/metrics", promhttp.Handler())
	return http.ListenAndServe(listen, nil)
}

func (s *Splitter) Shutdown() error {
	return nil
}

func (s *Splitter) startWithListener(listen net.Listener) error {
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

	e.Use(svcutil.MetricsMiddleware)
	e.HTTPErrorHandler = s.errorHandler

	e.POST("/xrpc/com.atproto.sync.requestCrawl", s.HandleComAtprotoSyncRequestCrawl)
	e.GET("/xrpc/com.atproto.sync.subscribeRepos", s.HandleSubscribeRepos)

	// proxy endpoints to upstream (relay)
	e.GET("/xrpc/com.atproto.sync.listRepos", s.ProxyRequestUpstream)
	e.GET("/xrpc/com.atproto.sync.getRepoStatus", s.ProxyRequestUpstream)
	e.GET("/xrpc/com.atproto.sync.getLatestCommit", s.ProxyRequestUpstream)
	e.GET("/xrpc/com.atproto.sync.listHosts", s.ProxyRequestUpstream)
	e.GET("/xrpc/com.atproto.sync.getHostStatus", s.ProxyRequestUpstream)
	e.GET("/xrpc/com.atproto.sync.getRepo", s.ProxyRequestUpstream)

	// proxy endpoint to collectiondir
	e.GET("/xrpc/com.atproto.sync.listReposByCollection", s.ProxyRequestCollectionDir)

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

func (s *Splitter) errorHandler(err error, ctx echo.Context) {
	switch err := err.(type) {
	case *echo.HTTPError:
		if err2 := ctx.JSON(err.Code, map[string]any{
			"error": err.Message,
		}); err2 != nil {
			s.logger.Error("Failed to write http error", "err", err2)
		}
	default:
		sendHeader := true
		if ctx.Path() == "/xrpc/com.atproto.sync.subscribeRepos" {
			sendHeader = false
		}

		s.logger.Warn("HANDLER ERROR", "path", ctx.Path(), "err", err)

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

func (s *Splitter) getLastCursor() (int64, error) {
	if s.pp != nil {
		seq, millis, _, err := s.pp.GetLast(context.Background())
		if err == nil {
			s.logger.Debug("got last cursor from pebble", "seq", seq, "millis", millis)
			return seq, nil
		} else if errors.Is(err, pebblepersist.ErrNoLast) {
			s.logger.Info("pebble no last")
		} else {
			s.logger.Error("pebble seq fail", "err", err)
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

func sleepForBackoff(b int) time.Duration {
	if b == 0 {
		return 0
	}

	if b < 50 {
		return time.Millisecond * time.Duration(rand.Intn(100)+(5*b))
	}

	return time.Second * 5
}

func (s *Splitter) subscribeWithRedialer(ctx context.Context, cursor int64) {
	d := websocket.Dialer{}

	upstreamUrl, err := url.Parse(s.conf.UpstreamHostWebsocket())
	if err != nil {
		panic(err) // this should have been checked in NewSplitter
	}
	upstreamUrl = upstreamUrl.JoinPath("/xrpc/com.atproto.sync.subscribeRepos")

	header := http.Header{
		"User-Agent": []string{s.conf.UserAgent},
	}

	var backoff int
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		var uurl string
		if cursor < 0 {
			upstreamUrl.RawQuery = ""
			uurl = upstreamUrl.String()
		} else {
			upstreamUrl.RawQuery = fmt.Sprintf("cursor=%d", cursor)
			uurl = upstreamUrl.String()
		}
		con, res, err := d.DialContext(ctx, uurl, header)
		if err != nil {
			s.logger.Warn("dialing failed", "url", uurl, "err", err, "backoff", backoff)
			time.Sleep(sleepForBackoff(backoff))
			backoff++

			continue
		}

		s.logger.Info("event subscription response", "code", res.StatusCode)

		if err := s.handleUpstreamConnection(ctx, con, &cursor); err != nil {
			s.logger.Warn("upstream connection failed", "url", uurl, "err", err)
		}
	}
}

func (s *Splitter) handleUpstreamConnection(ctx context.Context, con *websocket.Conn, lastCursor *int64) error {
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
				s.logger.Error("write cursor failed", "err", err)
			}
		}

		*lastCursor = seq
		return nil
	})

	return events.HandleRepoStream(ctx, con, sched, nil)
}
