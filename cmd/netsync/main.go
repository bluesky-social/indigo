package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	logging "github.com/ipfs/go-log"
	_ "github.com/joho/godotenv/autoload"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"golang.org/x/time/rate"

	"github.com/bluesky-social/indigo/util/version"
	"github.com/urfave/cli/v2"
)

var log = logging.Logger("netsync")

func main() {
	app := cli.App{
		Name:    "netsync",
		Usage:   "atproto network cloning tool",
		Version: version.Version,
	}

	app.Flags = []cli.Flag{
		&cli.IntFlag{
			Name:  "port",
			Usage: "listen port for metrics server",
			Value: 8753,
		},
		&cli.IntFlag{
			Name:  "worker-count",
			Usage: "number of workers to run concurrently",
			Value: 10,
		},
		&cli.Float64Flag{
			Name:  "checkout-limit",
			Usage: "maximum number of repos per second to checkout",
			Value: 4,
		},
		&cli.StringFlag{
			Name:  "out-dir",
			Usage: "directory to write cloned repos to",
			Value: "netsync-out",
		},
		&cli.StringFlag{
			Name:  "repo-list",
			Usage: "path to file containing list of repos to clone",
			Value: "repos.txt",
		},
		&cli.StringFlag{
			Name:  "state-file",
			Usage: "path to file to write state to",
			Value: "state.json",
		},
		&cli.StringFlag{
			Name:  "checkout-path",
			Usage: "path to checkout endpoint",
			Value: "https://bgs.bsky.social/xrpc/com.atproto.sync.getRepo",
		},
		&cli.StringFlag{
			Name:    "magic-header-key",
			Usage:   "header key to send with checkout request",
			Value:   "",
			EnvVars: []string{"MAGIC_HEADER_KEY"},
		},
		&cli.StringFlag{
			Name:    "magic-header-val",
			Usage:   "header value to send with checkout request",
			Value:   "",
			EnvVars: []string{"MAGIC_HEADER_VAL"},
		},
	}

	app.Commands = []*cli.Command{
		{
			Name:  "retry",
			Usage: "requeue failed repos",
			Action: func(cctx *cli.Context) error {
				state := &NetsyncState{
					StatePath: cctx.String("state-file"),
				}

				err := state.Resume()
				if err != nil {
					return err
				}

				// Look through finished repos for failed ones
				for _, repoState := range state.FinishedRepos {
					// Don't retry repos that failed due to a 400 (they've been deleted)
					if strings.HasPrefix(repoState.State, "failed") && repoState.State != "failed (status: 400)" {
						state.EnqueuedRepos[repoState.Repo] = &RepoState{
							Repo:  repoState.Repo,
							State: "enqueued",
						}
					}
				}

				// Save state
				return state.Save()
			},
		},
		{
			Name:   "playback",
			Usage:  "playback the contents of a netsync output directory",
			Action: Playback,
			Flags: []cli.Flag{
				&cli.StringSliceFlag{
					Name:    "scylla-nodes",
					Usage:   "list of scylla nodes to connect to",
					EnvVars: []string{"SCYLLA_NODES"},
				},
			},
		},
		{
			Name:   "query",
			Usage:  "run a test query against scylla",
			Action: Query,
			Flags: []cli.Flag{
				&cli.StringSliceFlag{
					Name:    "scylla-nodes",
					Usage:   "list of scylla nodes to connect to",
					EnvVars: []string{"SCYLLA_NODES"},
				},
			},
		},
		{
			Name:   "getPostsForUser",
			Usage:  "run a test query against scylla",
			Action: GetPostsForUser,
			Flags: []cli.Flag{
				&cli.StringSliceFlag{
					Name:    "scylla-nodes",
					Usage:   "list of scylla nodes to connect to",
					EnvVars: []string{"SCYLLA_NODES"},
				},
			},
		},
	}

	app.Action = Netsync

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

type RepoState struct {
	Repo       string
	State      string
	FinishedAt time.Time
}

type NetsyncState struct {
	EnqueuedRepos map[string]*RepoState
	FinishedRepos map[string]*RepoState
	StatePath     string
	CheckoutPath  string

	outDir         string
	magicHeaderKey string
	magicHeaderVal string

	lk          sync.RWMutex
	wg          sync.WaitGroup
	exit        chan struct{}
	limiter     *rate.Limiter
	workerCount int
	client      *http.Client
}

type instrumentedReader struct {
	source  io.ReadCloser
	counter prometheus.Counter
}

func (r instrumentedReader) Read(b []byte) (int, error) {
	n, err := r.source.Read(b)
	r.counter.Add(float64(n))
	return n, err
}

func (r instrumentedReader) Close() error {
	var buf [32]byte
	var n int
	var err error
	for err == nil {
		n, err = r.source.Read(buf[:])
		r.counter.Add(float64(n))
	}
	closeerr := r.source.Close()
	if err != nil && err != io.EOF {
		return err
	}
	return closeerr
}

func (s *NetsyncState) Save() error {
	s.lk.RLock()
	defer s.lk.RUnlock()

	stateFile, err := os.OpenFile(s.StatePath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer stateFile.Close()

	stateBytes, err := json.Marshal(s)
	if err != nil {
		return err
	}

	_, err = stateFile.Write(stateBytes)
	return err
}

func (s *NetsyncState) Resume() error {
	stateFile, err := os.Open(s.StatePath)
	if err != nil {
		return err
	}

	stateBytes, err := io.ReadAll(stateFile)
	if err != nil {
		return err
	}

	err = json.Unmarshal(stateBytes, s)
	if err != nil {
		return err
	}

	return nil
}

var enqueuedJobs = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "netsync_enqueued_jobs",
	Help: "Number of enqueued jobs",
})

func (s *NetsyncState) Dequeue() string {
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

var finishedJobs = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "netsync_finished_jobs",
	Help: "Number of finished jobs",
})

func (s *NetsyncState) Finish(repo string, state string) {
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

func Netsync(cctx *cli.Context) error {
	ctx := cctx.Context
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	state := &NetsyncState{
		StatePath:    cctx.String("state-file"),
		CheckoutPath: cctx.String("checkout-path"),

		outDir:         cctx.String("out-dir"),
		workerCount:    cctx.Int("worker-count"),
		limiter:        rate.NewLimiter(rate.Limit(cctx.Float64("checkout-limit")), 1),
		magicHeaderKey: cctx.String("magic-header-key"),
		magicHeaderVal: cctx.String("magic-header-val"),

		exit: make(chan struct{}),
		wg:   sync.WaitGroup{},
		client: &http.Client{
			Timeout: 180 * time.Second,
		},
	}

	if state.magicHeaderKey != "" && state.magicHeaderVal != "" {
		log.Info("using magic header")
	}

	// Create out dir
	err := os.MkdirAll(state.outDir, 0755)
	if err != nil {
		return err
	}

	// Try to resume from state file
	err = state.Resume()
	if state.EnqueuedRepos == nil {
		state.EnqueuedRepos = make(map[string]*RepoState)
	} else {
		// Reset any dequeued repos
		for _, repoState := range state.EnqueuedRepos {
			if repoState.State == "dequeued" {
				repoState.State = "enqueued"
			}
		}
	}

	if state.FinishedRepos == nil {
		state.FinishedRepos = make(map[string]*RepoState)
	}

	if err != nil {
		// Read repo list
		repoListFile, err := os.Open(cctx.String("repo-list"))
		if err != nil {
			return err
		}

		fileScanner := bufio.NewScanner(repoListFile)
		fileScanner.Split(bufio.ScanLines)

		for fileScanner.Scan() {
			repo := fileScanner.Text()
			state.EnqueuedRepos[repo] = &RepoState{
				Repo:  repo,
				State: "enqueued",
			}
		}
	} else {
		log.Info("Resuming from state file")
	}

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

	// Start workers
	for i := 0; i < state.workerCount; i++ {
		state.wg.Add(1)
		go func(id int) {
			defer state.wg.Done()
			err := state.worker(id)
			if err != nil {
				log.Errorw("worker failed", "err", err)
			}
		}(i)
	}

	// Check for empty queue
	go func() {
		state.wg.Add(1)
		defer state.wg.Done()
		t := time.NewTicker(30 * time.Second)
		for {
			select {
			case <-ctx.Done():
				err := state.Save()
				if err != nil {
					log.Errorw("failed to save state", "err", err)
				}
				return
			case <-t.C:
				err := state.Save()
				if err != nil {
					log.Errorw("failed to save state", "err", err)
				}
				state.lk.RLock()
				if len(state.EnqueuedRepos) == 0 {
					log.Info("no more repos to clone, shutting down")
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

	log.Info("shut down successfully")

	return nil

}

func (s *NetsyncState) worker(id int) error {
	log := log.With("worker", id)
	log.Infow("starting worker")
	defer log.Infow("worker stopped")
	for {
		select {
		case <-s.exit:
			log.Info("worker exiting due to exit signal")
			return nil
		default:
			ctx := context.Background()
			// Dequeue repo
			repo := s.Dequeue()
			if repo == "" {
				// No more repos to clone
				return nil
			}

			// Wait for rate limiter
			s.limiter.Wait(ctx)

			// Clone repo
			cloneState, err := s.cloneRepo(ctx, repo)
			if err != nil {
				log.Errorw("failed to clone repo", "repo", repo, "err", err)
			}

			// Update state
			s.Finish(repo, cloneState)
			log.Infow("worker finished", "repo", repo, "status", cloneState)
		}
	}
}

var repoCloneDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name: "netsync_repo_clone_duration_seconds",
	Help: "Duration of repo clone operations",
}, []string{"status"})

var bytesProcessed = promauto.NewCounter(prometheus.CounterOpts{
	Name: "netsync_bytes_processed",
	Help: "Number of bytes processed",
})

func (s *NetsyncState) cloneRepo(ctx context.Context, repo string) (cloneState string, err error) {
	log := log.With("repo", repo, "source", "cloneRepo")
	log.Infow("cloning repo")

	start := time.Now()
	defer func() {
		duration := time.Since(start)
		repoCloneDuration.WithLabelValues(cloneState).Observe(duration.Seconds())
	}()

	var url = fmt.Sprintf("%s?did=%s", s.CheckoutPath, repo)

	// Clone repo
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		cloneState = "failed (request-creation)"
		return cloneState, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.ipld.car")
	req.Header.Set("User-Agent", "jaz-atproto-netsync/0.0.1")
	if s.magicHeaderKey != "" && s.magicHeaderVal != "" {
		req.Header.Set(s.magicHeaderKey, s.magicHeaderVal)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		cloneState = "failed (client.do)"
		return cloneState, fmt.Errorf("failed to get repo: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		cloneState = fmt.Sprintf("failed (status: %d)", resp.StatusCode)
		return cloneState, fmt.Errorf("failed to get repo: %s", resp.Status)
	}

	instrumentedReader := instrumentedReader{
		source:  resp.Body,
		counter: bytesProcessed,
	}
	defer instrumentedReader.Close()

	// Write to file
	outPath := fmt.Sprintf("%s/%s", s.outDir, repo)
	outFile, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		cloneState = "failed (file.open)"
		return cloneState, fmt.Errorf("failed to open file: %w", err)
	}

	_, err = io.Copy(outFile, instrumentedReader)
	if err != nil {
		cloneState = "failed (file.copy)"
		return cloneState, fmt.Errorf("failed to copy file: %w", err)
	}

	err = outFile.Close()
	if err != nil {
		cloneState = "failed (file.close)"
		return cloneState, fmt.Errorf("failed to close file: %w", err)
	}
	cloneState = "success"
	return cloneState, nil
}
