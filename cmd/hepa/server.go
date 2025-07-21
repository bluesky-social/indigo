package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/automod/cachestore"
	"github.com/bluesky-social/indigo/automod/countstore"
	"github.com/bluesky-social/indigo/automod/engine"
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
	Engine      *automod.Engine
	RedisClient *redis.Client

	logger *slog.Logger
}

type Config struct {
	Logger               *slog.Logger
	BskyHost             string
	OzoneHost            string
	OzoneDID             string
	OzoneAdminToken      string
	PDSHost              string
	PDSAdminToken        string
	SetsFileJSON         string
	RedisURL             string
	SlackWebhookURL      string
	HiveAPIToken         string
	AbyssHost            string
	AbyssPassword        string
	RulesetName          string
	RatelimitBypass      string
	PreScreenHost        string
	PreScreenToken       string
	ReportDupePeriod     time.Duration
	QuotaModReportDay    int
	QuotaModTakedownDay  int
	QuotaModActionDay    int
	RecordEventTimeout   time.Duration
	IdentityEventTimeout time.Duration
	OzoneEventTimeout    time.Duration
}

func NewServer(dir identity.Directory, config Config) (*Server, error) {
	logger := config.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
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
	eng := automod.Engine{
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
		Config: engine.EngineConfig{
			ReportDupePeriod:     config.ReportDupePeriod,
			QuotaModReportDay:    config.QuotaModReportDay,
			QuotaModTakedownDay:  config.QuotaModTakedownDay,
			QuotaModActionDay:    config.QuotaModActionDay,
			RecordEventTimeout:   config.RecordEventTimeout,
			IdentityEventTimeout: config.IdentityEventTimeout,
			OzoneEventTimeout:    config.OzoneEventTimeout,
		},
	}

	s := &Server{
		logger:      logger,
		Engine:      &eng,
		RedisClient: rdb,
	}

	return s, nil
}

func (s *Server) RunMetrics(listen string) error {
	http.Handle("/metrics", promhttp.Handler())
	return http.ListenAndServe(listen, nil)
}
