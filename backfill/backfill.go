package backfill

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	// Blank import to register types for CBORGEN
	_ "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/repomgr"
	"github.com/ipfs/go-cid"
	typegen "github.com/whyrusleeping/cbor-gen"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// Job is an interface for a backfill job
type Job interface {
	Repo() string
	State() string
	SetState(ctx context.Context, state string) error

	// FlushBufferedOps calls the given callback for each buffered operation
	// Once done it clears the buffer and marks the job as "complete"
	// Allowing the Job interface to abstract away the details of how buffered
	// operations are stored and/or locked
	FlushBufferedOps(ctx context.Context, cb func(kind, path string, rec *typegen.CBORMarshaler, cid *cid.Cid) error) error

	ClearBufferedOps(ctx context.Context) error
}

// Store is an interface for a backfill store which holds Jobs
type Store interface {
	// BufferOp buffers an operation for a job and returns true if the operation was buffered
	// If the operation was not buffered, it returns false and an error (ErrJobNotFound or ErrJobComplete)
	BufferOp(ctx context.Context, repo string, kind, path string, rec *typegen.CBORMarshaler, cid *cid.Cid) (bool, error)
	GetJob(ctx context.Context, repo string) (Job, error)
	GetNextEnqueuedJob(ctx context.Context) (Job, error)
}

// Backfiller is a struct which handles backfilling a repo
type Backfiller struct {
	Name               string
	HandleCreateRecord func(ctx context.Context, repo string, path string, rec *typegen.CBORMarshaler, cid *cid.Cid) error
	HandleUpdateRecord func(ctx context.Context, repo string, path string, rec *typegen.CBORMarshaler, cid *cid.Cid) error
	HandleDeleteRecord func(ctx context.Context, repo string, path string) error
	Store              Store

	// Number of backfills to process in parallel
	ParallelBackfills int
	// Number of records to process in parallel for each backfill
	ParallelRecordCreates int
	// Prefix match for records to backfill i.e. app.bsky.feed.app/
	// If empty, all records will be backfilled
	NSIDFilter   string
	CheckoutPath string

	Logger      *zap.SugaredLogger
	syncLimiter *rate.Limiter

	magicHeaderKey string
	magicHeaderVal string

	stop chan chan struct{}
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

type BackfillOptions struct {
	ParallelBackfills     int
	ParallelRecordCreates int
	NSIDFilter            string
	SyncRequestsPerSecond int
	CheckoutPath          string
}

func DefaultBackfillOptions() *BackfillOptions {
	return &BackfillOptions{
		ParallelBackfills:     10,
		ParallelRecordCreates: 100,
		NSIDFilter:            "",
		SyncRequestsPerSecond: 2,
		CheckoutPath:          "https://bsky.social/xrpc/com.atproto.sync.getCheckout",
	}
}

// NewBackfiller creates a new Backfiller
func NewBackfiller(
	name string,
	store Store,
	handleCreate func(ctx context.Context, repo string, path string, rec *typegen.CBORMarshaler, cid *cid.Cid) error,
	handleUpdate func(ctx context.Context, repo string, path string, rec *typegen.CBORMarshaler, cid *cid.Cid) error,
	handleDelete func(ctx context.Context, repo string, path string) error,
	logger *zap.SugaredLogger,
	opts *BackfillOptions,
) *Backfiller {
	if opts == nil {
		opts = DefaultBackfillOptions()
	}
	return &Backfiller{
		Name:                  name,
		Store:                 store,
		HandleCreateRecord:    handleCreate,
		HandleUpdateRecord:    handleUpdate,
		HandleDeleteRecord:    handleDelete,
		ParallelBackfills:     opts.ParallelBackfills,
		ParallelRecordCreates: opts.ParallelRecordCreates,
		NSIDFilter:            opts.NSIDFilter,
		Logger:                logger,
		syncLimiter:           rate.NewLimiter(rate.Limit(opts.SyncRequestsPerSecond), 1),
		CheckoutPath:          opts.CheckoutPath,
		stop:                  make(chan chan struct{}),
	}
}

// Start starts the backfill processor routine
func (b *Backfiller) Start() {
	ctx := context.Background()

	log := b.Logger.With("source", "backfiller_main")
	log.Info("starting backfill processor")

	sem := make(chan struct{}, b.ParallelBackfills)

	for {
		select {
		case stopped := <-b.stop:
			log.Info("stopping backfill processor")
			close(stopped)
			return
		default:
		}

		// Get the next job
		job, err := b.Store.GetNextEnqueuedJob(ctx)
		if err != nil {
			log.Errorf("failed to get next backfill: %+v", err)
			time.Sleep(1 * time.Second)
			continue
		} else if job == nil {
			time.Sleep(1 * time.Second)
			continue
		}

		// Mark the backfill as "in progress"
		err = job.SetState(ctx, StateInProgress)
		if err != nil {
			log.Errorf("failed to set backfill state: %+v", err)
			continue
		}

		sem <- struct{}{}
		go func(j Job) {
			b.BackfillRepo(ctx, j)
			backfillJobsProcessed.WithLabelValues(b.Name).Inc()
			<-sem
		}(job)
	}
}

// Stop stops the backfill processor
func (b *Backfiller) Stop() {
	b.Logger.Info("stopping backfill processor")
	stopped := make(chan struct{})
	b.stop <- stopped
	<-stopped
	b.Logger.Info("backfill processor stopped")
}

// FlushBuffer processes buffered operations for a job
func (b *Backfiller) FlushBuffer(ctx context.Context, job Job) int {
	ctx, span := tracer.Start(ctx, "FlushBuffer")
	defer span.End()
	log := b.Logger.With("source", "backfiller_buffer_flush", "repo", job.Repo())

	processed := 0

	repo := job.Repo()

	// Flush buffered operations, clear the buffer, and mark the job as "complete"
	// Clearning and marking are handled by the job interface
	err := job.FlushBufferedOps(ctx, func(kind, path string, rec *typegen.CBORMarshaler, cid *cid.Cid) error {
		switch repomgr.EventKind(kind) {
		case repomgr.EvtKindCreateRecord:
			err := b.HandleCreateRecord(ctx, repo, path, rec, cid)
			if err != nil {
				log.Errorf("failed to handle create record: %+v", err)
			}
		case repomgr.EvtKindUpdateRecord:
			err := b.HandleUpdateRecord(ctx, repo, path, rec, cid)
			if err != nil {
				log.Errorf("failed to handle update record: %+v", err)
			}
		case repomgr.EvtKindDeleteRecord:
			err := b.HandleDeleteRecord(ctx, repo, path)
			if err != nil {
				log.Errorf("failed to handle delete record: %+v", err)
			}
		}
		backfillOpsBuffered.WithLabelValues(b.Name).Dec()
		processed++
		return nil
	})
	if err != nil {
		log.Errorf("failed to flush buffered ops: %+v", err)
	}

	return processed
}

type recordQueueItem struct {
	recordPath string
	nodeCid    cid.Cid
}

type recordResult struct {
	recordPath string
	err        error
}

// BackfillRepo backfills a repo
func (b *Backfiller) BackfillRepo(ctx context.Context, job Job) {
	ctx, span := tracer.Start(ctx, "BackfillRepo")
	defer span.End()

	start := time.Now()

	repoDid := job.Repo()

	log := b.Logger.With("source", "backfiller_backfill_repo", "repo", repoDid)
	log.Infof("processing backfill for %s", repoDid)

	var url = fmt.Sprintf("%s?did=%s", b.CheckoutPath, repoDid)

	// GET and CAR decode the body
	client := &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
		Timeout:   120 * time.Second,
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		log.Errorf("Error creating request: %v", err)
		return
	}

	req.Header.Set("Accept", "application/vnd.ipld.car")
	req.Header.Set("User-Agent", fmt.Sprintf("atproto-backfill-%s/0.0.1", b.Name))
	if b.magicHeaderKey != "" && b.magicHeaderVal != "" {
		req.Header.Set(b.magicHeaderKey, b.magicHeaderVal)
	}

	b.syncLimiter.Wait(ctx)

	resp, err := client.Do(req)
	if err != nil {
		log.Errorf("Error sending request: %v", err)
		return
	}

	if resp.StatusCode != http.StatusOK {
		log.Errorf("Error response: %v", resp.StatusCode)
		reason := "unknown error"
		if resp.StatusCode == http.StatusBadRequest {
			reason = "repo not found"
		}
		state := fmt.Sprintf("failed (%s)", reason)

		// Mark the job as "failed"
		err := job.SetState(ctx, state)
		if err != nil {
			log.Errorf("failed to set job state: %+v", err)
		}

		// Clear buffered ops
		err = job.ClearBufferedOps(ctx)
		if err != nil {
			log.Errorf("failed to clear buffered ops: %+v", err)
		}
		return
	}

	instrumentedReader := instrumentedReader{
		source:  resp.Body,
		counter: backfillBytesProcessed.WithLabelValues(b.Name),
	}

	defer instrumentedReader.Close()

	r, err := repo.ReadRepoFromCar(ctx, instrumentedReader)
	if err != nil {
		log.Errorf("Error reading repo: %v", err)

		state := "failed (couldn't read repo CAR from response body)"

		// Mark the job as "failed"
		err := job.SetState(ctx, state)
		if err != nil {
			log.Errorf("failed to set job state: %+v", err)
		}

		// Clear buffered ops
		err = job.ClearBufferedOps(ctx)
		if err != nil {
			log.Errorf("failed to clear buffered ops: %+v", err)
		}
		return
	}

	numRecords := 0
	numRoutines := b.ParallelRecordCreates
	recordQueue := make(chan recordQueueItem, numRoutines)
	recordResults := make(chan recordResult, numRoutines)

	wg := sync.WaitGroup{}

	// Producer routine
	go func() {
		defer close(recordQueue)
		r.ForEach(ctx, b.NSIDFilter, func(recordPath string, nodeCid cid.Cid) error {
			numRecords++
			recordQueue <- recordQueueItem{recordPath: recordPath, nodeCid: nodeCid}
			return nil
		})
	}()

	// Consumer routines
	for i := 0; i < numRoutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range recordQueue {
				recordCid, rec, err := r.GetRecord(ctx, item.recordPath)
				if err != nil {
					recordResults <- recordResult{recordPath: item.recordPath, err: fmt.Errorf("failed to get record: %w", err)}
					continue
				}

				// Verify that the record cid matches the cid in the event
				if recordCid != item.nodeCid {
					recordResults <- recordResult{recordPath: item.recordPath, err: fmt.Errorf("mismatch in record and op cid: %s != %s", recordCid, item.nodeCid)}
					continue
				}

				err = b.HandleCreateRecord(ctx, repoDid, item.recordPath, &rec, &recordCid)
				if err != nil {
					recordResults <- recordResult{recordPath: item.recordPath, err: fmt.Errorf("failed to handle create record: %w", err)}
					continue
				}

				backfillRecordsProcessed.WithLabelValues(b.Name).Inc()
				recordResults <- recordResult{recordPath: item.recordPath, err: err}
			}
		}()
	}

	resultWG := sync.WaitGroup{}
	resultWG.Add(1)
	// Handle results
	go func() {
		defer resultWG.Done()
		for result := range recordResults {
			if result.err != nil {
				log.Errorf("Error processing record %s: %v", result.recordPath, result.err)
			}
		}
	}()

	wg.Wait()
	close(recordResults)
	resultWG.Wait()

	// Process buffered operations, marking the job as "complete" when done
	numProcessed := b.FlushBuffer(ctx, job)

	log.Infow("backfill complete", "buffered_records_processed", numProcessed, "records_backfilled", numRecords, "duration", time.Since(start))
}
