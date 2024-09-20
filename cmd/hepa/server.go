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

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
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
	relayHost           string
	firehoseParallelism int
	logger              *slog.Logger
	engine              *automod.Engine
	rdb                 *redis.Client

	// lastSeq is the most recent event sequence number we've received and begun to handle.
	// This number is periodically persisted to redis, if redis is present.
	// The value is best-effort (the stream handling itself is concurrent, so event numbers may not be monotonic),
	// but nonetheless, you must use atomics when updating or reading this (to avoid data races).
	lastSeq int64

	// same as lastSeq, but for Ozone timestamp cursor. the value is a string.
	lastOzoneCursor atomic.Value
}

type Config struct {
	Logger              *slog.Logger
	RelayHost           string
	BskyHost            string
	OzoneHost           string
	OzoneDID            string
	OzoneAdminToken     string
	PDSHost             string
	PDSAdminToken       string
	SetsFileJSON        string
	RedisURL            string
	SlackWebhookURL     string
	HiveAPIToken        string
	AbyssHost           string
	AbyssPassword       string
	RulesetName         string
	RatelimitBypass     string
	FirehoseParallelism int
	PreScreenHost       string
	PreScreenToken      string
}

func NewServer(dir identity.Directory, config Config) (*Server, error) {
	logger := config.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
	}

	relayws := config.RelayHost
	if !strings.HasPrefix(relayws, "ws") {
		return nil, fmt.Errorf("specified relay host must include 'ws://' or 'wss://'")
	}

	var ozoneClient *xrpc.Client
	if config.OzoneAdminToken != "" && config.OzoneDID != "" {
		ozoneClient = &xrpc.Client{
			Client:     util.RobustHTTPClient(),
			Host:       config.OzoneHost,
			AdminToken: &config.OzoneAdminToken,
			Auth:       &xrpc.AuthInfo{},
		}
		if config.RatelimitBypass != "" {
			ozoneClient.Headers = make(map[string]string)
			ozoneClient.Headers["x-ratelimit-bypass"] = config.RatelimitBypass
		}
		od, err := syntax.ParseDID(config.OzoneDID)
		if err != nil {
			return nil, fmt.Errorf("ozone account DID supplied was not valid: %v", err)
		}
		ozoneClient.Auth.Did = od.String()
		logger.Info("configured ozone admin client", "did", od.String(), "ozoneHost", config.OzoneHost)
	} else {
		logger.Info("did not configure ozone client")
	}

	var adminClient *xrpc.Client
	if config.PDSAdminToken != "" {
		adminClient = &xrpc.Client{
			Client:     util.RobustHTTPClient(),
			Host:       config.PDSHost,
			AdminToken: &config.PDSAdminToken,
			Auth:       &xrpc.AuthInfo{},
		}
		if config.RatelimitBypass != "" {
			adminClient.Headers = make(map[string]string)
			adminClient.Headers["x-ratelimit-bypass"] = config.RatelimitBypass
		}
		logger.Info("configured PDS admin client", "pdsHost", config.PDSHost)
	} else {
		logger.Info("did not configure PDS admin client")
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

		csh, err := cachestore.NewRedisCacheStore(config.RedisURL, 6*time.Hour)
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
		cache = cachestore.NewMemCacheStore(5_000, 1*time.Hour)
		flags = flagstore.NewMemFlagStore()
	}

	// IMPORTANT: reminder that these are the indigo-edition rules, not production rules
	extraBlobRules := []automod.BlobRuleFunc{}
	if config.HiveAPIToken != "" && config.RulesetName != "no-hive" {
		logger.Info("configuring Hive AI image labeler")
		hc := visual.NewHiveAIClient(config.HiveAPIToken)
		extraBlobRules = append(extraBlobRules, hc.HiveLabelBlobRule)

		if config.PreScreenHost != "" {
			psc := visual.NewPreScreenClient(config.PreScreenHost, config.PreScreenToken)
			hc.PreScreenClient = psc
		}
	}

	if config.AbyssHost != "" && config.AbyssPassword != "" {
		logger.Info("configuring abyss abusive image scanning")
		ac := visual.NewAbyssClient(config.AbyssHost, config.AbyssPassword, config.RatelimitBypass)
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

	bskyClient := xrpc.Client{
		Client: util.RobustHTTPClient(),
		Host:   config.BskyHost,
	}
	if config.RatelimitBypass != "" {
		bskyClient.Headers = make(map[string]string)
		bskyClient.Headers["x-ratelimit-bypass"] = config.RatelimitBypass
	}
	blobClient := util.RobustHTTPClient()
	engine := automod.Engine{
		Logger:      logger,
		Directory:   dir,
		Counters:    counters,
		Sets:        sets,
		Flags:       flags,
		Cache:       cache,
		Rules:       ruleset,
		Notifier:    notifier,
		BskyClient:  &bskyClient,
		OzoneClient: ozoneClient,
		AdminClient: adminClient,
		BlobClient:  blobClient,
	}

	s := &Server{
		relayHost:           config.RelayHost,
		firehoseParallelism: config.FirehoseParallelism,
		logger:              logger,
		engine:              &engine,
		rdb:                 rdb,
	}

	return s, nil
}

func (s *Server) RunMetrics(listen string) error {
	http.Handle("/metrics", promhttp.Handler())
	return http.ListenAndServe(listen, nil)
}

var cursorKey = "hepa/seq"
var ozoneCursorKey = "hepa/ozoneTimestamp"

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
	} else if err != nil {
		return 0, err
	}
	s.logger.Info("successfully found prior subscription cursor seq in redis", "seq", val)
	return val, nil
}

func (s *Server) ReadLastOzoneCursor(ctx context.Context) (string, error) {
	// if redis isn't configured, just skip
	if s.rdb == nil {
		s.logger.Info("redis not configured, skipping ozone cursor read")
		return "", nil
	}

	val, err := s.rdb.Get(ctx, ozoneCursorKey).Result()
	if err == redis.Nil || val == "" {
		s.logger.Info("no pre-existing ozone cursor in redis")
		return "", nil
	} else if err != nil {
		return "", err
	}
	s.logger.Info("successfully found prior ozone offset timestamp in redis", "cursor", val)
	return val, nil
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

func (s *Server) PersistOzoneCursor(ctx context.Context) error {
	// if redis isn't configured, just skip
	if s.rdb == nil {
		return nil
	}
	lastCursor := s.lastOzoneCursor.Load()
	if lastCursor == nil || lastCursor == "" {
		return nil
	}
	err := s.rdb.Set(ctx, ozoneCursorKey, lastCursor, 14*24*time.Hour).Err()
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

// this method runs in a loop, persisting the current cursor state every 5 seconds
func (s *Server) RunPersistOzoneCursor(ctx context.Context) error {

	// if redis isn't configured, just skip
	if s.rdb == nil {
		return nil
	}
	ticker := time.NewTicker(5 * time.Second)
	for {
		select {
		case <-ctx.Done():
			lastCursor := s.lastOzoneCursor.Load()
			if lastCursor != nil && lastCursor != "" {
				s.logger.Info("persisting final ozone cursor timestamp", "cursor", lastCursor)
				err := s.PersistOzoneCursor(ctx)
				if err != nil {
					s.logger.Error("failed to persist ozone cursor", "err", err, "cursor", lastCursor)
				}
			}
			return nil
		case <-ticker.C:
			lastCursor := s.lastOzoneCursor.Load()
			if lastCursor != nil && lastCursor != "" {
				err := s.PersistOzoneCursor(ctx)
				if err != nil {
					s.logger.Error("failed to persist ozone cursor", "err", err, "cursor", lastCursor)
				}
			}
		}
	}
}
