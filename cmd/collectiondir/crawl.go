package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"github.com/bluesky-social/indigo/util"
	"golang.org/x/time/rate"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync/atomic"

	"github.com/urfave/cli/v2"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/xrpc"
)

type DidCollection struct {
	Did        string `json:"d"`
	Collection string `json:"c"`
}

func DidCollectionsToCsv(out io.Writer, sources <-chan DidCollection) {
	writer := csv.NewWriter(out)
	defer writer.Flush()
	var row [2]string
	for dc := range sources {
		row[0] = dc.Did
		row[1] = dc.Collection
		writer.Write(row[:])
	}
}

var offlineCrawlCmd = &cli.Command{
	Name:  "offline_crawl",
	Usage: "crawl a PDS to csv out",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "host",
			Usage: "hostname or URL of PDS",
		},
		&cli.StringFlag{
			Name:  "csv-out",
			Usage: "path for output or - for stdout",
		},
		&cli.Float64Flag{
			Name:  "qps",
			Usage: "queries per second to do vs target PDS",
			Value: 50, // large PDS: 500_000 repos, 10_000 seconds, ~3 hours
		},
		&cli.StringFlag{
			Name:    "ratelimit-header",
			Usage:   "secret for friend PDSes",
			EnvVars: []string{"BSKY_SOCIAL_RATE_LIMIT_SKIP", "RATE_LIMIT_HEADER"},
		},
	},
	Action: func(cctx *cli.Context) error {
		log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		hostname := cctx.String("host")
		hosturl, err := url.Parse(hostname)
		if err != nil {
			hosturl = new(url.URL)
			hosturl.Scheme = "https"
			hosturl.Host = hostname
		}
		rpcClient := xrpc.Client{
			Host:   hosturl.String(),
			Client: util.RobustHTTPClient(),
		}
		if cctx.IsSet("ratelimit-header") {
			rpcClient.Headers = map[string]string{
				"x-ratelimit-bypass": cctx.String("ratelimit-header"),
			}
		}
		log.Info("will crawl", "url", rpcClient.Host)
		csvOutPath := cctx.String("csv-out")
		var fout io.Writer = os.Stdout
		if csvOutPath != "" {
			if csvOutPath == "-" {
				fout = os.Stdout
			} else {
				fout, err = os.Create(csvOutPath)
				if err != nil {
					return fmt.Errorf("%s: could not open for writing: %w", csvOutPath, err)
				}
			}
		}
		qps := cctx.Float64("qps")
		results := make(chan DidCollection, 100)
		defer close(results)
		go DidCollectionsToCsv(fout, results)
		crawler := Crawler{
			Ctx:       ctx,
			RpcClient: &rpcClient,
			QPS:       qps,
			Results:   results,
			Log:       log,
		}
		err = crawler.CrawlPDSRepoCollections()
		log.Info("done")

		return err
	},
}

type Crawler struct {
	Ctx       context.Context
	RpcClient *xrpc.Client
	QPS       float64
	Results   chan<- DidCollection
	Log       *slog.Logger
	Stats     *CrawlStats
}

type CrawlStats struct {
	ReposDescribed atomic.Uint32
}

// CrawlPDSRepoCollections
// write results to chan
// does _not_ close chan
// (allow multiple threads of PDS queries running to one output chan, e.g. feeding into SetFromResults() )
func (cr *Crawler) CrawlPDSRepoCollections() error {
	var cursor string
	limiter := rate.NewLimiter(rate.Limit(cr.QPS), 1)
	for {
		limiter.Wait(cr.Ctx)
		repos, err := atproto.SyncListRepos(cr.Ctx, cr.RpcClient, cursor, 1000)
		if err != nil {
			return fmt.Errorf("%s: sync repos: %w", cr.RpcClient.Host, err)
		}
		slog.Debug("got repo list", "count", len(repos.Repos))
		for _, xr := range repos.Repos {
			limiter.Wait(cr.Ctx)
			desc, err := atproto.RepoDescribeRepo(cr.Ctx, cr.RpcClient, xr.Did)
			if err != nil {
				erst := err.Error()
				if strings.Contains(erst, "RepoDeactivated") || strings.Contains(erst, "RepoTakendown") {
					slog.Info("repo unavail", "host", cr.RpcClient.Host, "did", xr.Did, "err", err)
				} else {
					slog.Warn("repo desc", "host", cr.RpcClient.Host, "did", xr.Did, "err", err)
				}
				continue
			}
			for _, collection := range desc.Collections {
				cr.Results <- DidCollection{Did: xr.Did, Collection: collection}
			}
			if cr.Stats != nil {
				cr.Stats.ReposDescribed.Add(1)
			}
		}
		if repos.Cursor != nil {
			cursor = *repos.Cursor
		} else {
			break
		}
	}
	return nil
}
