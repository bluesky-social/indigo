package next

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/repo"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ipfs/go-cid"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"golang.org/x/time/rate"
)

// Job is an interface for a backfill job
type Job interface {
	Repo() string
	State() string
	SetState(ctx context.Context, state string) error
	RetryCount() int
}

// Store is an interface for a backfill store which holds Jobs
type Store interface {
	GetJob(ctx context.Context, repo string) (Job, error)
	GetNextEnqueuedJob(ctx context.Context, pds string) (Job, error)
	EnqueueJob(ctx context.Context, pds, repo string) error
}

var (
	// StateEnqueued is the state of a backfill job when it is first created
	StateEnqueued = "enqueued"
	// StateInProgress is the state of a backfill job when it is being processed
	StateInProgress = "in_progress"
	// StateComplete is the state of a backfill job when it has been processed
	StateComplete = "complete"
)

// ErrJobComplete is returned when trying to buffer an op for a job that is complete
var ErrJobComplete = errors.New("job is complete")

// ErrJobNotFound is returned when trying to buffer an op for a job that doesn't exist
var ErrJobNotFound = errors.New("job not found")

var tracer = otel.Tracer("backfiller")

// A Backfiller is a generic backfiller that can handle concurrent backfill jobs across multiple PDS instances.
type Backfiller struct {
	Name string

	HandleCreateRecord func(ctx context.Context, repo string, rev string, path string, rec *[]byte, cid *cid.Cid) error
	Store              Store

	perPDSBackfillConcurrency     int
	perPDSSyncsPerSecond          int
	globalRecordCreateConcurrency int

	globalRecordCreationLimiter *rate.Limiter

	NSIDFilter string

	pdsBackfillers map[string]*PDSBackfiller
	lk             sync.Mutex

	stop chan struct{}
}

type BackfillerOptions struct {
	PerPDSBackfillConcurrency     int
	PerPDSSyncsPerSecond          int
	GlobalRecordCreateConcurrency int
	NSIDFilter                    string
	Client                        *http.Client
}

func DefaultBackfillerOptions() *BackfillerOptions {
	return &BackfillerOptions{
		PerPDSBackfillConcurrency:     10,
		GlobalRecordCreateConcurrency: 100,
		NSIDFilter:                    "",
		PerPDSSyncsPerSecond:          2,
	}
}

func NewBackfiller(
	name string,
	store Store,
	handleCreate func(ctx context.Context, repo string, rev string, path string, rec *[]byte, cid *cid.Cid) error,
	opts *BackfillerOptions,
) *Backfiller {
	if opts == nil {
		opts = DefaultBackfillerOptions()
	}

	return &Backfiller{
		Name:                          name,
		Store:                         store,
		HandleCreateRecord:            handleCreate,
		perPDSBackfillConcurrency:     opts.PerPDSBackfillConcurrency,
		perPDSSyncsPerSecond:          opts.PerPDSSyncsPerSecond,
		globalRecordCreateConcurrency: opts.GlobalRecordCreateConcurrency,
		globalRecordCreationLimiter:   rate.NewLimiter(rate.Limit(opts.GlobalRecordCreateConcurrency), opts.GlobalRecordCreateConcurrency),
		NSIDFilter:                    opts.NSIDFilter,
		pdsBackfillers:                make(map[string]*PDSBackfiller),
		stop:                          make(chan struct{}),
	}
}

func (b *Backfiller) EnqueueJob(ctx context.Context, pds, repo string) error {
	log := slog.With("component", "backfiller", "name", b.Name, "pds", pds, "repo", repo)
	log.Debug("enqueueing backfill job")

	if err := b.Store.EnqueueJob(ctx, pds, repo); err != nil {
		log.Error("failed to enqueue backfill job", "error", err)
		return err
	}

	// Check if we already have a backfiller for this PDS
	b.lk.Lock()
	defer b.lk.Unlock()
	if _, exists := b.pdsBackfillers[pds]; !exists {
		log.Info("creating new PDS backfiller", "pds", pds)
		opts := DefaultPDSBackfillerOptions()
		opts.ParallelRecordCreates = 2
		opts.SyncRequestsPerSecond = b.perPDSSyncsPerSecond
		opts.RecordCreateLimiter = b.globalRecordCreationLimiter
		opts.NSIDFilter = b.NSIDFilter

		if strings.HasSuffix(pds, ".host.bsky.network") {
			opts.Client.Timeout = 600 * time.Second
			opts.ParallelBackfills = b.perPDSBackfillConcurrency
		} else {
			opts.ParallelBackfills = int(math.Min(2, float64(b.perPDSBackfillConcurrency)))
		}

		pdsBackfiller := NewPDSBackfiller(pds, pds, b.Store, b.HandleCreateRecord, opts)
		b.pdsBackfillers[pds] = pdsBackfiller
		pdsBackfiller.Start()
	}
	backfillJobsEnqueued.WithLabelValues(b.Name).Inc()
	log.Debug("backfill job enqueued successfully")
	return nil
}

func (b *Backfiller) Shutdown(ctx context.Context) error {
	log := slog.With("component", "backfiller", "name", b.Name)
	log.Info("shutting down backfiller")
	close(b.stop)
	b.lk.Lock()
	defer b.lk.Unlock()
	// Concurrently stop all PDS backfillers
	var wg sync.WaitGroup
	for _, pdsBackfiller := range b.pdsBackfillers {
		wg.Add(1)
		go func(pds *PDSBackfiller) {
			defer wg.Done()
			pds.Stop(ctx)
		}(pdsBackfiller)
	}
	wg.Wait()
	log.Info("all PDS backfillers stopped")
	return nil
}

type PDSBackfiller struct {
	Name     string
	Hostname string
	client   *http.Client

	HandleCreateRecord func(ctx context.Context, repo string, rev string, path string, rec *[]byte, cid *cid.Cid) error
	Store              Store

	backfillConcurrency int
	syncLimiter         *rate.Limiter

	recordCreateConcurrency int
	recordCreateLimiter     *rate.Limiter
	recordsProcessed        prometheus.Counter

	NSIDFilter string

	wg   sync.WaitGroup
	stop chan struct{}
}

type PDSBackfillerOptions struct {
	ParallelBackfills     int
	ParallelRecordCreates int
	RecordCreateLimiter   *rate.Limiter
	NSIDFilter            string
	SyncRequestsPerSecond int
	Client                *http.Client
}

func DefaultPDSBackfillerOptions() *PDSBackfillerOptions {
	return &PDSBackfillerOptions{
		ParallelBackfills:     10,
		ParallelRecordCreates: 2,
		RecordCreateLimiter:   rate.NewLimiter(rate.Limit(100), 100),
		NSIDFilter:            "",
		SyncRequestsPerSecond: 2,
		Client: &http.Client{
			Transport: otelhttp.NewTransport(http.DefaultTransport),
			Timeout:   120 * time.Second,
		},
	}
}

// NewPDSBackfiller creates a new backfiller for a single PDS instance
func NewPDSBackfiller(
	name string,
	hostname string,
	store Store,
	handleCreate func(ctx context.Context, repo string, rev string, path string, rec *[]byte, cid *cid.Cid) error,
	opts *PDSBackfillerOptions,
) *PDSBackfiller {
	if opts == nil {
		opts = DefaultPDSBackfillerOptions()
	}

	return &PDSBackfiller{
		Name:                    name,
		Hostname:                hostname,
		Store:                   store,
		HandleCreateRecord:      handleCreate,
		backfillConcurrency:     opts.ParallelBackfills,
		recordCreateConcurrency: opts.ParallelRecordCreates,
		recordsProcessed:        backfillRecordsProcessed.WithLabelValues(name),
		NSIDFilter:              opts.NSIDFilter,
		syncLimiter:             rate.NewLimiter(rate.Limit(opts.SyncRequestsPerSecond), opts.SyncRequestsPerSecond),
		recordCreateLimiter:     opts.RecordCreateLimiter,
		stop:                    make(chan struct{}),
		client:                  opts.Client,
		wg:                      sync.WaitGroup{},
	}
}

// Start starts the backfill processor routine
func (b *PDSBackfiller) Start() {
	ctx := context.Background()

	log := slog.With("component", "backfiller", "name", b.Name, "hostname", b.Hostname)
	log.Info("starting backfill processor for PDS", "hostname", b.Hostname)

	// Start a producer to enqueue jobs
	jobs := make(chan Job)

	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		defer close(jobs)
		log := log.With("subcomponent", "producer")
		for {
			select {
			case <-b.stop:
				log.Info("stopping backfill producer")
				return
			default:
			}

			// Get the next job
			job, err := b.Store.GetNextEnqueuedJob(ctx, b.Hostname)
			if err != nil {
				log.Error("failed to get next enqueued job", "error", err)
				time.Sleep(5 * time.Second)
				continue
			} else if job == nil {
				time.Sleep(5 * time.Second)
				continue
			}
			jobs <- job
		}
	}()

	// Start the worker processes
	for i := 0; i < b.backfillConcurrency; i++ {
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			log := log.With("subcomponent", "worker", "worker_id", i)
			defer log.Info("stopping backfill worker")

			for job := range jobs {
				select {
				case <-b.stop:
					return
				default:
				}

				log := log.With("job", job.Repo(), "state", job.State())
				log.Debug("processing backfill job")

				if err := job.SetState(ctx, StateInProgress); err != nil {
					log.Error("failed to set job state to in_progress", "error", err)
					continue
				}

				newState, err := b.BackfillRepo(ctx, job)
				if err != nil {
					log.Error("failed to backfill repo", "error", err)
				} else {
					log.Debug("backfill job completed successfully", "new_state", newState)
				}

				if err := job.SetState(ctx, newState); err != nil {
					log.Error("failed to set job completion state", "error", err)
					continue
				}

				backfillJobsProcessed.WithLabelValues(b.Name).Inc()
			}
		}()
	}
}

// Stop stops the backfill processor
func (b *PDSBackfiller) Stop(ctx context.Context) {
	log := slog.With("source", "backfiller", "name", b.Name, "hostname", b.Hostname)
	log.Info("stopping PDS backfiller")
	close(b.stop)
	b.wg.Wait()
	log.Info("PDS backfiller stopped")
}

type recordQueueItem struct {
	recordPath string
	nodeCid    cid.Cid
}

type FetchRepoError struct {
	StatusCode int
	Status     string
}

func (e *FetchRepoError) Error() string {
	reason := "unknown error"
	if e.StatusCode == http.StatusBadRequest {
		reason = "repo not found"
	} else {
		reason = e.Status
	}
	return fmt.Sprintf("failed to get repo: %s (%d)", reason, e.StatusCode)
}

// BackfillRepo backfills a repo
func (b *PDSBackfiller) BackfillRepo(ctx context.Context, job Job) (string, error) {
	ctx, span := tracer.Start(ctx, "BackfillRepo")
	defer span.End()

	start := time.Now()

	repoDID := job.Repo()

	log := slog.With("source", "backfiller_backfill_repo", "repo", repoDID)
	if job.RetryCount() > 0 {
		log = log.With("retry_count", job.RetryCount())
	}
	log.Debug(fmt.Sprintf("processing backfill for %s", repoDID))

	r, err := b.fetchRepo(ctx, repoDID, b.Hostname)
	if err != nil {
		slog.Warn("repo CAR fetch from PDS failed", "did", repoDID, "pds", b.Hostname, "err", err)
		rfe, ok := err.(*FetchRepoError)
		if ok {
			return fmt.Sprintf("failed to fetch repo CAR from PDS (http %d:%s)", rfe.StatusCode, rfe.Status), err
		}
		return "failed to fetch repo CAR from PDS", err
	}

	numRecords := 0
	numRoutines := b.recordCreateConcurrency
	recordQueue := make(chan recordQueueItem, numRoutines)
	recordErrors := make(chan error, numRoutines)

	// Producer routine
	go func() {
		defer close(recordQueue)
		if err := r.ForEach(ctx, b.NSIDFilter, func(recordPath string, nodeCid cid.Cid) error {
			numRecords++
			recordQueue <- recordQueueItem{recordPath: recordPath, nodeCid: nodeCid}
			return nil
		}); err != nil {
			log.Error("failed to iterate records in repo", "err", err)
		}
	}()

	rev := r.SignedCommit().Rev

	// Consumer routines
	wg := sync.WaitGroup{}
	for i := 0; i < numRoutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range recordQueue {
				blk, err := r.Blockstore().Get(ctx, item.nodeCid)
				if err != nil {
					recordErrors <- fmt.Errorf("failed to get blocks for record %q: %w", item.recordPath, err)
					continue
				}

				raw := blk.RawData()

				if err := b.recordCreateLimiter.Wait(ctx); err != nil {
					recordErrors <- fmt.Errorf("failed to wait for record create limiter %q: %w", item.recordPath, err)
					break
				}

				err = b.HandleCreateRecord(ctx, repoDID, rev, item.recordPath, &raw, &item.nodeCid)
				if err != nil {
					recordErrors <- fmt.Errorf("failed to handle create record %q: %w", item.recordPath, err)
					continue
				}

				b.recordsProcessed.Inc()
				recordErrors <- nil
			}
		}()
	}

	resultWG := sync.WaitGroup{}
	resultWG.Add(1)
	// Handle results
	go func() {
		defer resultWG.Done()
		for err := range recordErrors {
			if err != nil {
				log.Error("Error processing record", "error", err)
			}
		}
	}()

	wg.Wait()
	close(recordErrors)
	resultWG.Wait()

	log.Debug("backfill complete",
		"records_backfilled", numRecords,
		"duration", time.Since(start),
	)

	return StateComplete, nil
}

// Fetches a repo CAR file over HTTP from the indicated host. If successful, parses the CAR and returns repo.Repo
func (b *PDSBackfiller) fetchRepo(ctx context.Context, did, host string) (*repo.Repo, error) {
	url := fmt.Sprintf("https://%s/xrpc/com.atproto.sync.getRepo?did=%s", host, did)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.ipld.car")
	req.Header.Set("User-Agent", fmt.Sprintf("atproto-backfill-%s/2.0.0", b.Name))

	b.syncLimiter.Wait(ctx)

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &FetchRepoError{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
		}
	}

	instrumentedReader := instrumentedReader{
		source:  resp.Body,
		counter: backfillBytesProcessed.WithLabelValues(b.Name),
	}

	defer instrumentedReader.Close()

	repo, err := repo.ReadRepoFromCar(ctx, instrumentedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to parse repo from CAR file: %w", err)
	}
	return repo, nil
}
