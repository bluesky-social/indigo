package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"strings"
	"syscall"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/data"
	backfill "github.com/bluesky-social/indigo/backfill/next"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/ipfs/go-cid"
	slogGorm "github.com/orandin/slog-gorm"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/urfave/cli/v2"
	"golang.org/x/time/rate"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type Linear struct {
	db     *gorm.DB
	logger *slog.Logger

	bf       *backfill.Backfiller
	teardown chan struct{}

	out        *os.File
	outChan    chan []byte
	fileClosed chan struct{}
}

type Line struct {
	Repo       string          `json:"repo"`
	Collection string          `json:"collection"`
	RKey       string          `json:"rkey"`
	Cid        string          `json:"cid"`
	Record     json.RawMessage `json:"record"`
}

func main() {
	app := cli.NewApp()

	app.Flags = []cli.Flag{

		&cli.StringFlag{
			Name:    "metrics-listen",
			Usage:   "listen endpoint for metrics and pprof",
			Value:   "localhost:8081",
			EnvVars: []string{"LINEAR_METRICS_LISTEN"},
		},
	}
	app.Commands = []*cli.Command{
		{
			Name:   "sync",
			Action: Sync,
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:    "backfill-sqlite-path",
					Usage:   "path to the backfill SQLite database file",
					Value:   "data/backfill.db",
					EnvVars: []string{"LINEAR_BACKFILL_DB_PATH"},
				},
				&cli.StringSliceFlag{
					Name:    "pds-list",
					Usage:   "list of PDSs to backfill, can be specified multiple times",
					Value:   cli.NewStringSlice("blusher.us-east.host.bsky.network", "yellowfoot.us-west.host.bsky.network", "psathyrella.us-west.host.bsky.network", "hollowfoot.us-west.host.bsky.network", "fuzzyfoot.us-west.host.bsky.network", "panus.us-west.host.bsky.network", "mazegill.us-west.host.bsky.network", "pioppino.us-west.host.bsky.network", "waxcap.us-west.host.bsky.network", "elfcup.us-east.host.bsky.network"),
					EnvVars: []string{"LINEAR_PDS_LIST"},
				},
				&cli.StringFlag{
					Name:    "output-file",
					EnvVars: []string{"LINEAR_OUTPUT_FILE"},
					Value:   "data/linear.jsonl",
				},
			},
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		slog.Error("exited with error", "err", err)
		os.Exit(1)
	}
}

func Sync(cctx *cli.Context) error {
	ctx := cctx.Context
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	logLevel := slog.LevelInfo
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel, AddSource: true}))
	slog.SetDefault(slog.New(logger.Handler()))

	mlisten := cctx.String("metrics-listen")
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		if err := http.ListenAndServe(mlisten, nil); err != nil {
			logger.Error("failed to set up metrics listener", "err", err)
		}
	}()

	store, db, err := setupBackfillStore(cctx.Context, cctx.String("backfill-sqlite-path"))
	if err != nil {
		return fmt.Errorf("failed to setup backfill store: %w", err)
	}

	backfillOutFile, err := os.OpenFile(cctx.String("output-file"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open output file: %w", err)
	}

	linear := &Linear{
		db:     db,
		logger: logger,

		out:        backfillOutFile,
		outChan:    make(chan []byte, 100_000),
		fileClosed: make(chan struct{}),

		teardown: make(chan struct{}),
	}

	linear.startWriter()

	opts := backfill.DefaultBackfillerOptions()
	opts.GlobalRecordCreateConcurrency = 100_000
	opts.PerPDSSyncsPerSecond = 10
	opts.PerPDSBackfillConcurrency = 15

	bf := backfill.NewBackfiller("linear-backfiller-v2", store, linear.handleCreate, opts)

	linear.bf = bf

	err = store.LoadJobs(ctx)
	if err != nil {
		return fmt.Errorf("failed to load jobs from store: %w", err)
	}

	// Walk the PDS list's listRepos endpoints and add jobs to the backfiller
	pdsList := cctx.StringSlice("pds-list")

	go func() {
		for _, pds := range pdsList {
			go func(pds string) {
				logger.Info("enqueuing PDS for backfill", "pds", pds)

				xrpcc := xrpc.Client{}
				xrpcc.Host = fmt.Sprintf("https://%s", pds)

				listLimiter := rate.NewLimiter(5, 1)

				cursor := ""
				for {
					if err := listLimiter.Wait(ctx); err != nil {
						logger.Error("failed to wait for rate limiter", "pds", pds, "err", err)
						continue
					}

					page, err := comatproto.SyncListRepos(ctx, &xrpcc, cursor, 1000)
					if err != nil {
						logger.Error("failed to list repos for PDS", "pds", pds, "err", err)
						continue
					}

					for _, repo := range page.Repos {
						logger.Info("found repo to backfill", "pds", pds, "repo", repo.Did)

						if err := bf.EnqueueJob(ctx, pds, repo.Did); err != nil {
							logger.Error("failed to enqueue job for PDS", "pds", pds, "repo", repo.Did, "err", err)
						} else {
							logger.Info("enqueued job for PDS", "pds", pds, "repo", repo.Did)
						}
					}

					if page.Cursor == nil {
						logger.Info("no more repos to process for PDS", "pds", pds)
						break
					}

					cursor = *page.Cursor
				}
				logger.Info("finished enqueuing PDS for backfill", "pds", pds)
			}(pds)
		}
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	<-signals
	if err := linear.shutdown(ctx); err != nil {
		logger.Error("shutdown encountered an error", "err", err)
	}

	return nil

}

func (lin *Linear) shutdown(ctx context.Context) error {
	// first, close down all 'sources' of work
	lin.bf.Shutdown(ctx)
	close(lin.teardown)
	<-lin.fileClosed
	return nil
}

func (l *Linear) handleCreate(ctx context.Context, repo string, rev string, path string, recB *[]byte, cid *cid.Cid) error {
	col, rkey, err := splitPath(path)
	if err != nil {
		return err
	}

	asCbor, err := data.UnmarshalCBOR(*recB)
	if err != nil {
		return fmt.Errorf("failed to unmarshal record: %w", err)
	}

	recJSON, err := json.Marshal(asCbor)
	if err != nil {
		return fmt.Errorf("failed to marshal record to json: %w", err)
	}

	line := Line{
		Repo:       repo,
		Collection: col,
		RKey:       rkey,
		Cid:        cid.String(),
		Record:     recJSON,
	}

	lineB, err := json.Marshal(line)
	if err != nil {
		return fmt.Errorf("failed to marshal line to json: %w", err)
	}

	l.outChan <- lineB

	return nil
}

func setupBackfillStore(ctx context.Context, sqlitePath string) (*backfill.Gormstore, *gorm.DB, error) {
	bfdb, err := gorm.Open(sqlite.Open(sqlitePath), &gorm.Config{
		TranslateError: true,
		Logger:         slogGorm.New(slogGorm.SetLogLevel(slogGorm.ErrorLogType, slog.LevelDebug)),
	})
	if err != nil {
		return nil, nil, err
	}

	if err := bfdb.Exec("PRAGMA journal_mode=WAL;").Error; err != nil {
		return nil, nil, err
	}

	if err := bfdb.AutoMigrate(&backfill.GormDBJob{}); err != nil {
		return nil, nil, err
	}

	store := backfill.NewGormstore(bfdb)
	if err := store.LoadJobs(ctx); err != nil {
		return nil, nil, err
	}

	return store, bfdb, nil
}

func splitPath(p string) (string, string, error) {
	parts := strings.Split(p, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("path must be collection and rkey")
	}

	return parts[0], parts[1], nil
}

func (lin *Linear) startWriter() {
	log := lin.logger.With("source", "writer")
	log.Info("starting writer")
	newline := []byte("\n")

	// Start the writer
	go func() {
		for {
			select {
			case <-lin.teardown:
				if err := lin.out.Sync(); err != nil {
					log.Error("failed to sync output file", "err", err)
				}
				if err := lin.out.Close(); err != nil {
					log.Error("failed to close output file", "err", err)
				}
				close(lin.fileClosed)
				close(lin.outChan)
				return
			case line := <-lin.outChan:
				if _, err := lin.out.Write(line); err != nil {
					log.Error("failed to write line to output file", "err", err)
				}
				if _, err := lin.out.Write(newline); err != nil {
					log.Error("failed to write newline to output file", "err", err)
				}
			}
		}
	}()
}
