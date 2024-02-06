package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/automod/cachestore"
	"github.com/bluesky-social/indigo/automod/countstore"
	"github.com/bluesky-social/indigo/automod/flagstore"
	"github.com/bluesky-social/indigo/automod/rules"
	"github.com/bluesky-social/indigo/automod/setstore"
	"github.com/bluesky-social/indigo/automod/visual"
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

	// lastSeq is the most recent event sequence number we've received and begun to handle.
	// This number is periodically persisted to redis, if redis is present.
	// The value is best-effort (the stream handling itself is concurrent, so event numbers may not be monotonic),
	// but nonetheless, you must use atomics when updating or reading this (to avoid data races).
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
	HiveAPIToken    string
	AbyssHost       string
	AbyssPassword   string
	RulesetName     string
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

	sets := setstore.NewMemSetStore()
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

	extraBlobRules := []automod.BlobRuleFunc{}
	if config.HiveAPIToken != "" && config.RulesetName != "no-hive" {
		logger.Info("configuring Hive AI image labeler")
		hc := visual.NewHiveAIClient(config.HiveAPIToken)
		extraBlobRules = append(extraBlobRules, hc.HiveLabelBlobRule)
	}

	if config.AbyssHost != "" && config.AbyssPassword != "" {
		logger.Info("configuring abyss abusive image scanning")
		ac := visual.NewAbyssClient(config.AbyssHost, config.AbyssPassword)
		extraBlobRules = append(extraBlobRules, ac.AbyssScanBlobRule)
	}

	var ruleset automod.RuleSet
	switch config.RulesetName {
	case "", "default", "no-hive":
		ruleset = rules.DefaultRules()
		ruleset.BlobRules = append(ruleset.BlobRules, extraBlobRules...)
	case "no-blobs":
		ruleset = rules.DefaultRules()
		ruleset.BlobRules = []automod.BlobRuleFunc{}
	case "only-blobs":
		ruleset.BlobRules = extraBlobRules
	default:
		return nil, fmt.Errorf("unknown ruleset config: %s", config.RulesetName)
	}

	var notifier automod.Notifier
	if config.SlackWebhookURL != "" {
		notifier = &automod.SlackNotifier{
			SlackWebhookURL: config.SlackWebhookURL,
		}
	}
	engine := automod.Engine{
		Logger:      logger,
		Directory:   dir,
		Counters:    counters,
		Sets:        sets,
		Flags:       flags,
		Cache:       cache,
		Rules:       ruleset,
		Notifier:    notifier,
		AdminClient: xrpcc,
		BskyClient: &xrpc.Client{
			Client: util.RobustHTTPClient(),
			Host:   config.BskyHost,
		},
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
	lastSeq := atomic.LoadInt64(&s.lastSeq)
	if lastSeq <= 0 {
		return nil
	}
	err := s.rdb.Set(ctx, cursorKey, lastSeq, 14*24*time.Hour).Err()
	return err
}

// Periodically refreshes the engine's admin XRPC client JWT auth token.
//
// Expects to be run in a goroutine, and to be the only running code which touches the auth fields (aka, there is no locking).
// TODO: this is a hack until we have an XRPC client which handles these details automatically.
func (s *Server) RunRefreshAdminClient(ctx context.Context) error {
	if s.engine.AdminClient == nil {
		return nil
	}
	ac := s.engine.AdminClient
	ticker := time.NewTicker(1 * time.Hour)
	for {
		select {
		case <-ticker.C:
			// uses a temporary xrpc client instead of the existing one because we need to put refreshJwt in the position of accessJwt, and that would cause an error for any concurrent requests
			tmpClient := xrpc.Client{
				Host: ac.Host,
				Auth: &xrpc.AuthInfo{
					Did:        ac.Auth.Did,
					Handle:     ac.Auth.Handle,
					AccessJwt:  ac.Auth.RefreshJwt,
					RefreshJwt: ac.Auth.RefreshJwt,
				},
			}
			refresh, err := comatproto.ServerRefreshSession(ctx, &tmpClient)
			if err != nil {
				// don't return an error, just log, and attempt again on the next tick
				s.logger.Error("failed to refresh admin client session", "err", err, "host", ac.Host)
			} else {
				s.engine.AdminClient.Auth.RefreshJwt = refresh.RefreshJwt
				s.engine.AdminClient.Auth.AccessJwt = refresh.AccessJwt
				s.logger.Info("refreshed admin client session")
			}
		case <-ctx.Done():
			return nil
		}
	}
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
			lastSeq := atomic.LoadInt64(&s.lastSeq)
			if lastSeq >= 1 {
				s.logger.Info("persisting final cursor seq value", "seq", lastSeq)
				err := s.PersistCursor(ctx)
				if err != nil {
					s.logger.Error("failed to persist cursor", "err", err, "seq", lastSeq)
				}
			}
			return nil
		case <-ticker.C:
			lastSeq := atomic.LoadInt64(&s.lastSeq)
			if lastSeq >= 1 {
				err := s.PersistCursor(ctx)
				if err != nil {
					s.logger.Error("failed to persist cursor", "err", err, "seq", lastSeq)
				}
			}
		}
	}
}
