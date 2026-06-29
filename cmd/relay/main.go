package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "net/http/pprof"

	_ "github.com/joho/godotenv/autoload"
	_ "go.uber.org/automaxprocs"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/cmd/relay/relay"
	"github.com/bluesky-social/indigo/cmd/relay/stream/eventmgr"
	"github.com/bluesky-social/indigo/cmd/relay/stream/persist/diskpersist"
	"github.com/bluesky-social/indigo/util/cliutil"

	"github.com/earthboundkid/versioninfo/v2"
	"github.com/urfave/cli/v3"
	"gorm.io/plugin/opentelemetry/tracing"
)

func main() {
	if err := run(os.Args); err != nil {
		slog.Error("exiting process", "err", err.Error())
		os.Exit(-1)
	}
}

func run(args []string) error {

	app := cli.Command{
		Name:    "relay",
		Usage:   "atproto relay daemon",
		Version: versioninfo.Short(),
	}
	app.Flags = []cli.Flag{
		&cli.StringSliceFlag{
			Name:    "admin-password",
			Usage:   "secret password/token for accessing admin endpoints (multiple values allowed)",
			Sources: cli.EnvVars("RELAY_ADMIN_PASSWORD", "RELAY_ADMIN_KEY"),
		},
		&cli.StringFlag{
			Name:    "plc-host",
			Usage:   "method, hostname, and port of PLC registry",
			Value:   "https://plc.directory",
			Sources: cli.EnvVars("RELAY_PLC_HOST", "ATP_PLC_HOST"),
		},
		&cli.StringFlag{
			Name:    "log-level",
			Usage:   "log verbosity level (eg: warn, info, debug)",
			Sources: cli.EnvVars("RELAY_LOG_LEVEL", "GO_LOG_LEVEL", "LOG_LEVEL"),
		},
	}
	app.Commands = []*cli.Command{
		&cli.Command{
			Name:   "serve",
			Usage:  "run the relay daemon",
			Action: runRelay,
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:    "db-url",
					Usage:   "database connection string for relay database",
					Value:   "sqlite://data/relay/relay.sqlite",
					Sources: cli.EnvVars("DATABASE_URL"),
				},
				&cli.IntFlag{
					Name:    "max-db-conn",
					Usage:   "limit on size of database connection pool",
					Sources: cli.EnvVars("MAX_DB_CONNECTIONS", "MAX_METADB_CONNECTIONS"),
					Value:   40,
				},
				&cli.StringFlag{
					Name:    "bind",
					Usage:   "IP or address, and port, to listen on for HTTP APIs (including firehose)",
					Value:   ":2470",
					Sources: cli.EnvVars("RELAY_API_BIND", "RELAY_API_LISTEN"),
				},
				&cli.StringFlag{
					Name:    "persist-dir",
					Usage:   "local folder to store firehose playback files",
					Value:   "data/relay/persist",
					Sources: cli.EnvVars("RELAY_PERSIST_DIR", "RELAY_PERSISTER_DIR"),
				},
				&cli.DurationFlag{
					Name:    "replay-window",
					Usage:   "retention duration for firehose playback",
					Sources: cli.EnvVars("RELAY_REPLAY_WINDOW", "RELAY_EVENT_PLAYBACK_TTL"),
					Value:   72 * time.Hour,
				},
				&cli.IntFlag{
					Name:    "host-concurrency",
					Usage:   "number of concurrent worker routines per upstream host",
					Sources: cli.EnvVars("RELAY_HOST_CONCURRENCY", "RELAY_CONCURRENCY_PER_PDS"),
					Value:   40,
				},
				&cli.Int64Flag{
					Name:    "default-account-limit",
					Value:   100,
					Usage:   "max number of active accounts for new upstream hosts",
					Sources: cli.EnvVars("RELAY_DEFAULT_ACCOUNT_LIMIT", "RELAY_DEFAULT_REPO_LIMIT"),
				},
				&cli.Int64Flag{
					Name:    "new-hosts-per-day-limit",
					Value:   50,
					Usage:   "max number of new upstream hosts subscribed per day via public requestCrawl",
					Sources: cli.EnvVars("RELAY_NEW_HOSTS_PER_DAY_LIMIT"),
				},
				&cli.IntFlag{
					Name:    "ident-cache-size",
					Value:   5_000_000,
					Usage:   "size of in-process identity cache (eg, DID docs)",
					Sources: cli.EnvVars("RELAY_IDENT_CACHE_SIZE", "RELAY_DID_CACHE_SIZE"),
				},
				&cli.BoolFlag{
					Name:    "disable-request-crawl",
					Usage:   "don't process public (un-authenticated) com.atproto.sync.requestCrawl",
					Sources: cli.EnvVars("RELAY_DISABLE_REQUEST_CRAWL"),
				},
				&cli.BoolFlag{
					Name:    "allow-insecure-hosts",
					Usage:   "enables subscription to non-SSL hosts via requestCrawl",
					Sources: cli.EnvVars("RELAY_ALLOW_INSECURE_HOSTS"),
				},
				&cli.BoolFlag{
					Name:    "lenient-sync-validation",
					Usage:   "when messages fail atproto 'Sync 1.1' validation, just log, don't drop",
					Sources: cli.EnvVars("RELAY_LENIENT_SYNC_VALIDATION"),
				},
				&cli.Int64Flag{
					Name:    "initial-seq-number",
					Usage:   "output firehose seq will be greater or equal than this number (will jump ahead if needed)",
					Value:   1,
					Sources: cli.EnvVars("RELAY_INITIAL_SEQ_NUMBER"),
				},
				&cli.StringSliceFlag{
					Name:    "sibling-relays",
					Usage:   "servers (eg https://example.com) to forward admin state changes to; multiple allowed",
					Sources: cli.EnvVars("RELAY_SIBLING_RELAYS"),
				},
				&cli.StringSliceFlag{
					Name:    "trusted-domains",
					Usage:   "domain names which mark trusted hosts; use wildcard prefix to match suffixes",
					Value:   []string{"*.host.bsky.network"},
					Sources: cli.EnvVars("RELAY_TRUSTED_DOMAINS"),
				},
				&cli.StringFlag{
					Name:    "env",
					Value:   "dev",
					Sources: cli.EnvVars("ENVIRONMENT"),
					Usage:   "declared hosting environment (prod, qa, etc); used in metrics",
				},
				&cli.Float64Flag{
					Name:    "account-limit-alert-threshold",
					Value:   0.80,
					Sources: cli.EnvVars("RELAY_ACCOUNT_LIMIT_ALERT_THRESHOLD"),
					Usage:   "fraction of a PDS repo limit that triggers an alert",
				},
				&cli.DurationFlag{
					Name:    "account-limit-alert-interval",
					Value:   5 * time.Minute,
					Sources: cli.EnvVars("RELAY_ACCOUNT_LIMIT_ALERT_INTERVAL"),
					Usage:   "how often to check for PDS hosts approaching their repo limits",
				},
				&cli.DurationFlag{
					Name:    "account-limit-alert-repeat-interval",
					Value:   6 * time.Hour,
					Sources: cli.EnvVars("RELAY_ACCOUNT_LIMIT_ALERT_REPEAT_INTERVAL"),
					Usage:   "minimum interval between repeated alerts for a PDS host that remains over the alert threshold",
				},
				&cli.StringFlag{
					Name:    "slack-alert-channel",
					Sources: cli.EnvVars("RELAY_SLACK_ALERT_CHANNEL", "RELAY_ALERT_SLACK_CHANNEL"),
					Usage:   "Slack channel ID or name for PDS repo-limit alerts",
				},
				&cli.StringFlag{
					Name:    "slack-alert-token",
					Sources: cli.EnvVars("RELAY_SLACK_ALERT_TOKEN", "RELAY_ALERT_SLACK_TOKEN"),
					Usage:   "Slack bot token for PDS repo-limit alerts; environment variables are recommended",
				},
				&cli.BoolFlag{
					Name: "enable-db-tracing",
				},
				&cli.BoolFlag{
					Name: "enable-jaeger-tracing",
				},
				&cli.BoolFlag{
					Name: "enable-otel-tracing",
				},
				&cli.StringFlag{
					Name:    "metrics-listen",
					Usage:   "IP or address, and port, to listen on for prometheus metrics",
					Value:   ":2471",
					Sources: cli.EnvVars("RELAY_METRICS_LISTEN"),
				},
				&cli.StringFlag{
					Name:    "otel-exporter-otlp-endpoint",
					Value:   "http://localhost:4328",
					Sources: cli.EnvVars("OTEL_EXPORTER_OTLP_ENDPOINT"),
				},
			},
		},
		// additional commands defined in pull.go
		cmdPullHosts,
	}
	return app.Run(context.Background(), args)

}

func configLogger(cmd *cli.Command, writer io.Writer) *slog.Logger {
	var level slog.Level
	switch strings.ToLower(cmd.String("log-level")) {
	case "error":
		level = slog.LevelError
	case "warn":
		level = slog.LevelWarn
	case "info":
		level = slog.LevelInfo
	case "debug":
		level = slog.LevelDebug
	default:
		level = slog.LevelInfo
	}
	logger := slog.New(slog.NewJSONHandler(writer, &slog.HandlerOptions{
		Level: level,
	}))
	slog.SetDefault(logger)
	return logger
}

func safeDatabaseURLForLog(dburl string) string {
	if strings.HasPrefix(dburl, "sqlite://") || strings.HasPrefix(dburl, "sqlite=") {
		return dburl
	}
	if strings.HasPrefix(dburl, "postgres=") {
		return "postgres=<redacted>"
	}

	u, err := url.Parse(dburl)
	if err != nil || u.Scheme == "" {
		return "<redacted>"
	}
	if u.User != nil {
		username := u.User.Username()
		if username == "" {
			u.User = url.UserPassword("xxxxx", "xxxxx")
		} else {
			u.User = url.UserPassword(username, "xxxxx")
		}
	}
	if u.RawQuery != "" {
		q := u.Query()
		for k := range q {
			lowerKey := strings.ToLower(k)
			if strings.Contains(lowerKey, "pass") || strings.Contains(lowerKey, "token") || strings.Contains(lowerKey, "secret") {
				q.Set(k, "xxxxx")
			}
		}
		u.RawQuery = q.Encode()
	}
	return u.String()
}

func configureAccountLimitAlertMonitor(cmd *cli.Command, logger *slog.Logger, r *relay.Relay, sentCallback func(context.Context, AccountLimitAlertSentState)) (*AccountLimitAlertMonitor, error) {
	slackToken := strings.TrimSpace(cmd.String("slack-alert-token"))
	slackChannel := strings.TrimSpace(cmd.String("slack-alert-channel"))
	if slackToken == "" && slackChannel == "" {
		return nil, nil
	}
	if slackToken == "" || slackChannel == "" {
		return nil, fmt.Errorf("both slack alert token and slack alert channel are required when relay account-limit alerting is configured")
	}

	alerter, err := NewSlackAlerter(slackToken, slackChannel)
	if err != nil {
		return nil, err
	}
	config := DefaultAccountLimitAlertMonitorConfig()
	config.Threshold = cmd.Float64("account-limit-alert-threshold")
	config.CheckInterval = cmd.Duration("account-limit-alert-interval")
	config.RepeatInterval = cmd.Duration("account-limit-alert-repeat-interval")
	config.Environment = cmd.String("env")
	config.SentCallback = sentCallback

	monitor, err := NewAccountLimitAlertMonitor(logger, r, alerter, config)
	if err != nil {
		return nil, err
	}
	return monitor, nil
}

func runRelay(ctx context.Context, cmd *cli.Command) error {
	logger := configLogger(cmd, os.Stdout)

	// Trap SIGINT to trigger a shutdown.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	dburl := cmd.String("db-url")
	maxConn := cmd.Int("max-db-conn")
	logger.Info("configuring database", "url", safeDatabaseURLForLog(dburl), "maxConn", maxConn)
	db, err := cliutil.SetupDatabase(dburl, maxConn)
	if err != nil {
		return err
	}

	// TODO: add shared external cache
	baseDir := identity.BaseDirectory{
		SkipHandleVerification: true,
		SkipDNSDomainSuffixes:  []string{".bsky.social"},
		TryAuthoritativeDNS:    true,
		PLCURL:                 cmd.String("plc-host"),
	}
	dir := identity.NewCacheDirectory(&baseDir, cmd.Int("ident-cache-size"), time.Hour*24, time.Minute*2, time.Minute*5)

	persistDir := cmd.String("persist-dir")
	if err := os.MkdirAll(persistDir, os.ModePerm); err != nil {
		return err
	}
	persitConfig := diskpersist.DefaultDiskPersistOptions()
	persitConfig.Retention = cmd.Duration("replay-window")
	persitConfig.InitialSeq = cmd.Int64("initial-seq-number")
	if persitConfig.InitialSeq <= 0 {
		// belt-and-suspenders: the disk persister also checks this internally
		return fmt.Errorf("negative or zero initial sequence config: %d", persitConfig.InitialSeq)
	}
	logger.Info("setting up disk persister", "dir", persistDir, "replayWindow", persitConfig.Retention, "initialSeq", persitConfig.InitialSeq)
	persister, err := diskpersist.NewDiskPersistence(persistDir, "", db, persitConfig)
	if err != nil {
		return fmt.Errorf("setting up disk persister: %w", err)
	}

	relayConfig := relay.DefaultRelayConfig()
	relayConfig.UserAgent = fmt.Sprintf("indigo-relay/%s (atproto-relay)", versioninfo.Short())
	relayConfig.ConcurrencyPerHost = cmd.Int("host-concurrency")
	relayConfig.DefaultRepoLimit = cmd.Int64("default-account-limit")
	relayConfig.HostPerDayLimit = cmd.Int64("new-hosts-per-day-limit")
	relayConfig.TrustedDomains = cmd.StringSlice("trusted-domains")
	relayConfig.LenientSyncValidation = cmd.Bool("lenient-sync-validation")

	svcConfig := DefaultServiceConfig()
	svcConfig.AllowInsecureHosts = cmd.Bool("allow-insecure-hosts")
	svcConfig.DisableRequestCrawl = cmd.Bool("disable-request-crawl")
	svcConfig.SiblingRelayHosts = cmd.StringSlice("sibling-relays")
	if len(svcConfig.SiblingRelayHosts) > 0 {
		logger.Info("sibling relay hosts configured for admin state forwarding", "servers", svcConfig.SiblingRelayHosts)
	}
	if cmd.IsSet("admin-password") {
		svcConfig.AdminPasswords = cmd.StringSlice("admin-password")
	} else {
		var rblob [10]byte
		_, _ = rand.Read(rblob[:])
		randPassword := base64.URLEncoding.EncodeToString(rblob[:])
		svcConfig.AdminPasswords = []string{randPassword}
		logger.Info("generated random admin password", "username", "admin", "password", randPassword)
	}

	evtman := eventmgr.NewEventManager(persister)

	logger.Info("constructing relay service")
	r, err := relay.NewRelay(db, evtman, dir, relayConfig)
	if err != nil {
		return err
	}
	svc, err := NewService(r, svcConfig)
	if err != nil {
		return err
	}
	persister.SetUidSource(r)

	alertMonitor, err := configureAccountLimitAlertMonitor(cmd, logger, r, func(ctx context.Context, state AccountLimitAlertSentState) {
		go svc.ForwardSiblingAccountLimitAlertSent(ctx, state)
	})
	if err != nil {
		return err
	}
	if alertMonitor != nil {
		svc.SetAccountLimitAlertRecorder(alertMonitor)
	}
	alertCtx, cancelAlerts := context.WithCancel(ctx)
	alertDone := make(chan struct{})
	if alertMonitor != nil {
		logger.Info("starting PDS account-limit alert monitor", "threshold", cmd.Float64("account-limit-alert-threshold"), "interval", cmd.Duration("account-limit-alert-interval"), "repeatInterval", cmd.Duration("account-limit-alert-repeat-interval"), "slackChannel", cmd.String("slack-alert-channel"))
		go func() {
			defer close(alertDone)
			alertMonitor.Run(alertCtx)
		}()
	} else {
		close(alertDone)
	}
	var stopAlertsOnce sync.Once
	stopAlerts := func() {
		stopAlertsOnce.Do(func() {
			cancelAlerts()
			select {
			case <-alertDone:
			case <-time.After(5 * time.Second):
				logger.Warn("timed out waiting for account-limit alert monitor to stop")
			}
		})
	}
	defer stopAlerts()

	// start metrics endpoint
	go func() {
		if err := svc.StartMetrics(cmd.String("metrics-listen")); err != nil {
			logger.Error("failed to start metrics endpoint", "err", err)
			os.Exit(1)
		}
	}()

	// start observability/tracing (OTEL and jaeger)
	if err := setupOTEL(cmd); err != nil {
		return err
	}
	if cmd.Bool("enable-db-tracing") {
		if err := db.Use(tracing.NewPlugin()); err != nil {
			return err
		}
	}

	// restart any existing subscriptions as worker goroutines
	if err := r.ResubscribeAllHosts(ctx); err != nil {
		return err
	}

	svcErr := make(chan error, 1)
	go func() {
		err := svc.StartAPI(cmd.String("bind"))
		svcErr <- err
	}()

	logger.Info("startup complete")
	select {
	case <-signals:
		logger.Info("received shutdown signal")
		stopAlerts()
		errs := svc.Shutdown()
		for err := range errs {
			logger.Error("error during shutdown", "err", err)
		}
	case err := <-svcErr:
		if err != nil {
			logger.Error("error during startup", "err", err)
		}
		logger.Info("shutting down")
		stopAlerts()
		errs := svc.Shutdown()
		for err := range errs {
			logger.Error("error during shutdown", "err", err)
		}
	}

	logger.Info("shutdown complete")

	return nil
}
