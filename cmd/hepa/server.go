package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/automod/cachestore"
	"github.com/bluesky-social/indigo/automod/countstore"
	"github.com/bluesky-social/indigo/automod/flagstore"
	"github.com/bluesky-social/indigo/automod/rules"
	"github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
)

type Server struct {
	bgshost string
	logger  *slog.Logger
	engine  *automod.Engine
	rdb     *redis.Client
	lastSeq int64
}

type Config struct {
	BGSHost         string
	BskyHost        string
	ModHost         string
	ModAdminToken   string
	ModUsername     string
	ModPassword     string
	SetsFileJSON    string
	RedisURL        string
	SlackWebhookURL string
	Logger          *slog.Logger
}

func NewServer(dir identity.Directory, config Config) (*Server, error) {
	logger := config.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
	}

	bgsws := config.BGSHost
	if !strings.HasPrefix(bgsws, "ws") {
		return nil, fmt.Errorf("specified bgs host must include 'ws://' or 'wss://'")
	}

	// TODO: this isn't a very robust way to handle a persistent client
	var xrpcc *xrpc.Client
	if config.ModAdminToken != "" {
		xrpcc = &xrpc.Client{
			Client:     util.RobustHTTPClient(),
			Host:       config.ModHost,
			AdminToken: &config.ModAdminToken,
			Auth:       &xrpc.AuthInfo{},
		}

		auth, err := comatproto.ServerCreateSession(context.TODO(), xrpcc, &comatproto.ServerCreateSession_Input{
			Identifier: config.ModUsername,
			Password:   config.ModPassword,
		})
		if err != nil {
			return nil, fmt.Errorf("connecting to mod service: %v", err)
		}
		xrpcc.Auth.AccessJwt = auth.AccessJwt
		xrpcc.Auth.RefreshJwt = auth.RefreshJwt
		xrpcc.Auth.Did = auth.Did
		xrpcc.Auth.Handle = auth.Handle
	}

	sets := automod.NewMemSetStore()
	if config.SetsFileJSON != "" {
		if err := sets.LoadFromFileJSON(config.SetsFileJSON); err != nil {
			return nil, fmt.Errorf("initializing in-process setstore: %v", err)
		} else {
			logger.Info("loaded set config from JSON", "path", config.SetsFileJSON)
		}
	}

	var counters countstore.CountStore
	var cache cachestore.CacheStore
	var flags flagstore.FlagStore
	var rdb *redis.Client
	if config.RedisURL != "" {
		// generic client, for cursor state
		opt, err := redis.ParseURL(config.RedisURL)
		if err != nil {
			return nil, fmt.Errorf("parsing redis URL: %v", err)
		}
		rdb = redis.NewClient(opt)
		// check redis connection
		_, err = rdb.Ping(context.TODO()).Result()
		if err != nil {
			return nil, fmt.Errorf("redis ping failed: %v", err)
		}

		cnt, err := countstore.NewRedisCountStore(config.RedisURL)
		if err != nil {
			return nil, fmt.Errorf("initializing redis countstore: %v", err)
		}
		counters = cnt

		csh, err := cachestore.NewRedisCacheStore(config.RedisURL, 30*time.Minute)
		if err != nil {
			return nil, fmt.Errorf("initializing redis cachestore: %v", err)
		}
		cache = csh

		flg, err := flagstore.NewRedisFlagStore(config.RedisURL)
		if err != nil {
			return nil, fmt.Errorf("initializing redis flagstore: %v", err)
		}
		flags = flg
	} else {
		counters = countstore.NewMemCountStore()
		cache = cachestore.NewMemCacheStore(5_000, 30*time.Minute)
		flags = flagstore.NewMemFlagStore()
	}

	engine := automod.Engine{
		Logger:      logger,
		Directory:   dir,
		Counters:    counters,
		Sets:        sets,
		Flags:       flags,
		Cache:       cache,
		Rules:       rules.DefaultRules(),
		AdminClient: xrpcc,
		BskyClient: &xrpc.Client{
			Client: util.RobustHTTPClient(),
			Host:   config.BskyHost,
		},
		SlackWebhookURL: config.SlackWebhookURL,
	}

	s := &Server{
		bgshost: config.BGSHost,
		logger:  logger,
		engine:  &engine,
		rdb:     rdb,
	}

	return s, nil
}

func (s *Server) RunMetrics(listen string) error {
	http.Handle("/metrics", promhttp.Handler())
	return http.ListenAndServe(listen, nil)
}

var cursorKey = "hepa/seq"

func (s *Server) ReadLastCursor(ctx context.Context) (int64, error) {
	// if redis isn't configured, just skip
	if s.rdb == nil {
		s.logger.Info("redis not configured, skipping cursor read")
		return 0, nil
	}

	val, err := s.rdb.Get(ctx, cursorKey).Int64()
	if err == redis.Nil {
		s.logger.Info("no pre-existing cursor in redis")
		return 0, nil
	}
	s.logger.Info("successfully found prior subscription cursor seq in redis", "seq", val)
	return val, err
}

func (s *Server) PersistCursor(ctx context.Context) error {
	// if redis isn't configured, just skip
	if s.rdb == nil {
		return nil
	}
	if s.lastSeq <= 0 {
		return nil
	}
	err := s.rdb.Set(ctx, cursorKey, s.lastSeq, 14*24*time.Hour).Err()
	return err
}

// this method runs in a loop, persisting the current cursor state every 5 seconds
func (s *Server) RunPersistCursor(ctx context.Context) error {

	// if redis isn't configured, just skip
	if s.rdb == nil {
		return nil
	}
	ticker := time.NewTicker(5 * time.Second)
	for {
		select {
		case <-ctx.Done():
			if s.lastSeq >= 1 {
				s.logger.Info("persisting final cursor seq value", "seq", s.lastSeq)
				err := s.PersistCursor(ctx)
				if err != nil {
					s.logger.Error("failed to persist cursor", "err", err, "seq", s.lastSeq)
				}
			}
			return nil
		case <-ticker.C:
			if s.lastSeq >= 1 {
				err := s.PersistCursor(ctx)
				if err != nil {
					s.logger.Error("failed to persist cursor", "err", err, "seq", s.lastSeq)
				}
			}
		}
	}
}
