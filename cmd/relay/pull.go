package main

import (
	"errors"
	"fmt"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/cmd/relay/relay"
	"github.com/bluesky-social/indigo/cmd/relay/relay/models"
	"github.com/bluesky-social/indigo/util/cliutil"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/urfave/cli/v2"
)

var cmdPullHosts = &cli.Command{
	Name:   "pull-hosts",
	Usage:  "initializes or updates host list from an existing relay (public API)",
	Action: runPullHosts,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "relay-host",
			Usage:   "method, hostname, and port of relay to pull from",
			Value:   "https://bsky.network",
			EnvVars: []string{"RELAY_HOST"},
		},
		&cli.StringFlag{
			Name:    "db-url",
			Usage:   "database connection string for relay database",
			Value:   "sqlite://data/relay/relay.sqlite",
			EnvVars: []string{"DATABASE_URL"},
		},
		&cli.IntFlag{
			Name:    "default-account-limit",
			Value:   100,
			Usage:   "max number of active accounts for new upstream hosts",
			EnvVars: []string{"RELAY_DEFAULT_ACCOUNT_LIMIT", "RELAY_DEFAULT_REPO_LIMIT"},
		},
		&cli.IntFlag{
			Name:    "batch-size",
			Value:   500,
			Usage:   "host many hosts to pull at a time",
			EnvVars: []string{"RELAY_PULL_HOSTS_BATCH_SIZE"},
		},
		&cli.StringSliceFlag{
			Name:    "trusted-domains",
			Usage:   "domain names which mark trusted hosts; use wildcard prefix to match suffixes",
			Value:   cli.NewStringSlice("*.host.bsky.network"),
			EnvVars: []string{"RELAY_TRUSTED_DOMAINS"},
		},
		&cli.BoolFlag{
			Name:    "skip-host-checks",
			Usage:   "don't run describeServer requests to see if host is a PDS before adding",
			EnvVars: []string{"RELAY_SKIP_HOST_CHECKS"},
		},
	},
}

func runPullHosts(cctx *cli.Context) error {
	ctx := cctx.Context

	if cctx.Args().Len() > 0 {
		return fmt.Errorf("unexpected arguments")
	}

	client := xrpc.Client{
		Host: cctx.String("relay-host"),
	}

	skipHostChecks := cctx.Bool("skip-host-checks")

	dir := identity.DefaultDirectory()

	dburl := cctx.String("db-url")
	db, err := cliutil.SetupDatabase(dburl, 10)
	if err != nil {
		return err
	}

	relayConfig := relay.DefaultRelayConfig()
	relayConfig.DefaultRepoLimit = cctx.Int64("default-account-limit")
	relayConfig.TrustedDomains = cctx.StringSlice("trusted-domains")

	// NOTE: setting evtmgr to nil
	r, err := relay.NewRelay(db, nil, dir, relayConfig)
	if err != nil {
		return err
	}

	checker := relay.NewHostClient(relayConfig.UserAgent)

	cursor := ""
	size := cctx.Int64("batch-size")
	for {
		resp, err := comatproto.SyncListHosts(ctx, &client, cursor, size)
		if err != nil {
			return err
		}
		for _, h := range resp.Hosts {
			if h.Status == nil {
				fmt.Printf("%s: status=unknown\n", h.Hostname)
				continue
			}
			if !(models.HostStatus(*h.Status) == models.HostStatusActive || models.HostStatus(*h.Status) == models.HostStatusIdle) {
				fmt.Printf("%s: status=%s\n", h.Hostname, *h.Status)
				continue
			}
			if h.Seq == nil || *h.Seq <= 0 {
				fmt.Printf("%s: no-cursor\n", h.Hostname)
				continue
			}
			existing, err := r.GetHost(ctx, h.Hostname)
			if err != nil && !errors.Is(err, relay.ErrHostNotFound) {
				return err
			}
			if existing != nil {
				fmt.Printf("%s: exists\n", h.Hostname)
				continue
			}
			hostname, noSSL, err := relay.ParseHostname(h.Hostname)
			if err != nil {
				return fmt.Errorf("%w: %s", err, h.Hostname)
			}
			if noSSL {
				// skip "localhost" and non-SSL hosts (this is for public PDS instances)
				fmt.Printf("%s: non-public\n", h.Hostname)
				continue
			}

			accountLimit := r.Config.DefaultRepoLimit
			trusted := relay.IsTrustedHostname(hostname, r.Config.TrustedDomains)
			if trusted {
				accountLimit = r.Config.TrustedRepoLimit
			}

			if !skipHostChecks {
				if err := checker.CheckHost(ctx, "https://"+hostname); err != nil {
					fmt.Printf("%s: checking host: %s\n", h.Hostname, err)
					continue
				}
			}

			host := models.Host{
				Hostname:     hostname,
				NoSSL:        noSSL,
				Status:       models.HostStatusActive,
				Trusted:      trusted,
				AccountLimit: accountLimit,
			}
			if err := db.Create(&host).Error; err != nil {
				return err
			}
			fmt.Printf("%s: added\n", h.Hostname)
		}
		if resp.Cursor == nil || *resp.Cursor == "" {
			break
		}
		cursor = *resp.Cursor
	}
	return nil
}
