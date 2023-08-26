package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/repo"
	"github.com/ipfs/go-cid"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/urfave/cli/v2"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

type PlaybackState struct {
	EnqueuedRepos map[string]*RepoState
	FinishedRepos map[string]*RepoState

	outDir string

	lk          sync.RWMutex
	wg          sync.WaitGroup
	exit        chan struct{}
	workerCount int

	textLen atomic.Uint64
}

func (s *PlaybackState) Dequeue() string {
	s.lk.Lock()
	defer s.lk.Unlock()

	enqueuedJobs.Set(float64(len(s.EnqueuedRepos)))

	for repo, state := range s.EnqueuedRepos {
		if state.State == "enqueued" {
			state.State = "dequeued"
			return repo
		}
	}

	return ""
}

func (s *PlaybackState) Finish(repo string, state string) {
	s.lk.Lock()
	defer s.lk.Unlock()

	s.FinishedRepos[repo] = &RepoState{
		Repo:       repo,
		State:      state,
		FinishedAt: time.Now(),
	}

	finishedJobs.Set(float64(len(s.FinishedRepos)))

	delete(s.EnqueuedRepos, repo)
}

func Playback(cctx *cli.Context) error {
	ctx := cctx.Context
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	start := time.Now()

	state := &PlaybackState{
		outDir:      cctx.String("out-dir"),
		workerCount: cctx.Int("worker-count"),
		wg:          sync.WaitGroup{},
	}

	state.EnqueuedRepos = make(map[string]*RepoState)
	state.FinishedRepos = make(map[string]*RepoState)

	state.exit = make(chan struct{})

	// Start metrics server
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	metricsServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cctx.Int("port")),
		Handler: mux,
	}

	go func() {
		state.wg.Add(1)
		defer state.wg.Done()
		if err := metricsServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("failed to start metrics server: %+v", err)
		}
		log.Info("metrics server shut down successfully")
	}()

	// Load all the repos from the out dir
	err := filepath.Walk(state.outDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("failed to walk path: %w", err)
		}

		if info.IsDir() {
			return nil
		}

		state.EnqueuedRepos[info.Name()] = &RepoState{
			Repo:  info.Name(),
			State: "enqueued",
		}

		enqueuedJobs.Inc()

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to walk out dir: %w", err)
	}

	// Start workers
	for i := 0; i < state.workerCount; i++ {
		go state.worker(i)
	}

	// Check for empty queue
	go func() {
		state.wg.Add(1)
		defer state.wg.Done()
		t := time.NewTicker(30 * time.Second)
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				state.lk.RLock()
				if len(state.EnqueuedRepos) == 0 {
					log.Info("no more repos to process, shutting down")
					close(state.exit)
					return
				}
				state.lk.RUnlock()
			}
		}
	}()

	// Trap SIGINT to trigger a shutdown.
	log.Info("listening for signals")
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-signals:
		cancel()
		close(state.exit)
		log.Infof("shutting down on signal: %+v", sig)
	case <-ctx.Done():
		cancel()
		close(state.exit)
		log.Info("shutting down on context done")
	case <-state.exit:
		cancel()
		log.Info("shutting down on exit signal")
	}

	log.Info("shutting down, waiting for workers to clean up...")

	if err := metricsServer.Shutdown(ctx); err != nil {
		log.Errorf("failed to shut down metrics server: %+v", err)
	}

	state.wg.Wait()

	p := message.NewPrinter(language.English)

	// Print stats
	log.Info(p.Sprintf("processed %d repos and %d UTF-8 text characters in %s",
		len(state.FinishedRepos), state.textLen.Load(), time.Since(start)))
	log.Info("shut down successfully")

	return nil
}

func (s *PlaybackState) worker(id int) {
	log := log.With("worker", id)
	s.wg.Add(1)
	defer s.wg.Done()

	for {
		select {
		case <-s.exit:
			return
		default:
		}

		repo := s.Dequeue()
		if repo == "" {
			return
		}

		processState, err := s.processRepo(context.Background(), repo)
		if err != nil {
			log.Errorf("failed to process repo (%s): %v", repo, err)
		}

		s.Finish(repo, processState)
	}
}

func (s *PlaybackState) processRepo(ctx context.Context, did string) (processState string, err error) {
	log := log.With("repo", did)

	log.Debug("processing repo")

	// Open the repo file from the out dir
	f, err := os.Open(filepath.Join(s.outDir, did))
	if err != nil {
		return "", fmt.Errorf("failed to open repo file: %w", err)
	}
	defer f.Close()

	r, err := repo.ReadRepoFromCar(ctx, f)
	if err != nil {
		return "", fmt.Errorf("failed to read repo from car: %w", err)
	}

	r.ForEach(ctx, "", func(path string, nodeCid cid.Cid) error {
		recordCid, rec, err := r.GetRecord(ctx, path)
		if err != nil {
			return fmt.Errorf("failed to get record: %w", err)
		}

		// Verify that the record cid matches the cid in the event
		if recordCid != nodeCid {
			log.Errorf("mismatch in record and op cid: %s != %s", recordCid, nodeCid)
			return nil
		}

		switch rec := rec.(type) {
		case *bsky.FeedPost:
			log.Debugf("processing feed post: %s", rec.Text)
			s.textLen.Add(uint64(len(rec.Text)))
		case *bsky.FeedLike:
			log.Debugf("processing feed like: %s", rec.Subject.Uri)
		case *bsky.FeedRepost:
			log.Debugf("processing feed repost: %s", rec.Subject.Uri)
		case *bsky.GraphFollow:
			log.Debugf("processing graph follow: %s", rec.Subject)
		case *bsky.GraphBlock:
			log.Debugf("processing graph block: %s", rec.Subject)
		case *bsky.ActorProfile:
			if rec.DisplayName != nil {
				log.Debugf("processing actor profile: %s", *rec.DisplayName)
			}
		}

		return nil
	})

	return "finished", nil
}
