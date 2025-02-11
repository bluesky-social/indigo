package backfill

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/repomgr"

	"github.com/ipfs/go-cid"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"golang.org/x/sync/semaphore"
	"golang.org/x/time/rate"
)

// Job is an interface for a backfill job
type Job interface {
	Repo() string
	State() string
	Rev() string
	SetState(ctx context.Context, state string) error
	SetRev(ctx context.Context, rev string) error
	RetryCount() int

	// BufferOps buffers the given operations and returns true if the operations
	// were buffered.
	// The given operations move the repo from since to rev.
	BufferOps(ctx context.Context, since *string, rev string, ops []*BufferedOp) (bool, error)
	// FlushBufferedOps calls the given callback for each buffered operation
	// Once done it clears the buffer and marks the job as "complete"
	// Allowing the Job interface to abstract away the details of how buffered
	// operations are stored and/or locked
	FlushBufferedOps(ctx context.Context, cb func(kind repomgr.EventKind, rev, path string, rec *[]byte, cid *cid.Cid) error) error

	ClearBufferedOps(ctx context.Context) error
}

// Store is an interface for a backfill store which holds Jobs
type Store interface {
	// BufferOp buffers an operation for a job and returns true if the operation was buffered
	// If the operation was not buffered, it returns false and an error (ErrJobNotFound or ErrJobComplete)
	GetJob(ctx context.Context, repo string) (Job, error)
	GetNextEnqueuedJob(ctx context.Context) (Job, error)
	UpdateRev(ctx context.Context, repo, rev string) error

	EnqueueJob(ctx context.Context, repo string) error
	EnqueueJobWithState(ctx context.Context, repo string, state string) error

	PurgeRepo(ctx context.Context, repo string) error
}

// Backfiller is a struct which handles backfilling a repo
type Backfiller struct {
	Name               string
	HandleCreateRecord func(ctx context.Context, repo string, rev string, path string, rec *[]byte, cid *cid.Cid) error
	HandleUpdateRecord func(ctx context.Context, repo string, rev string, path string, rec *[]byte, cid *cid.Cid) error
	HandleDeleteRecord func(ctx context.Context, repo string, rev string, path string) error
	Store              Store

	// Number of backfills to process in parallel
	ParallelBackfills int
	// Number of records to process in parallel for each backfill
	ParallelRecordCreates int
	// Prefix match for records to backfill i.e. app.bsky.feed.app/
	// If empty, all records will be backfilled
	NSIDFilter string
	RelayHost  string

	syncLimiter *rate.Limiter

	magicHeaderKey string
	magicHeaderVal string

	tryRelayRepoFetch bool

	stop chan chan struct{}

	Directory identity.Directory
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

// ErrEventGap is returned when an event is received with a since that doesn't match the current rev
var ErrEventGap = fmt.Errorf("buffered event revs did not line up")

// ErrAlreadyProcessed is returned when attempting to buffer an event that has already been accounted for (rev older than current)
var ErrAlreadyProcessed = fmt.Errorf("event already accounted for")

var tracer = otel.Tracer("backfiller")

type BackfillOptions struct {
	ParallelBackfills     int
	ParallelRecordCreates int
	NSIDFilter            string
	SyncRequestsPerSecond int
	RelayHost             string
}

func DefaultBackfillOptions() *BackfillOptions {
	return &BackfillOptions{
		ParallelBackfills:     10,
		ParallelRecordCreates: 100,
		NSIDFilter:            "",
		SyncRequestsPerSecond: 2,
		RelayHost:             "https://bsky.network",
	}
}

// NewBackfiller creates a new Backfiller
func NewBackfiller(
	name string,
	store Store,
	handleCreate func(ctx context.Context, repo string, rev string, path string, rec *[]byte, cid *cid.Cid) error,
	handleUpdate func(ctx context.Context, repo string, rev string, path string, rec *[]byte, cid *cid.Cid) error,
	handleDelete func(ctx context.Context, repo string, rev string, path string) error,
	opts *BackfillOptions,
) *Backfiller {
	if opts == nil {
		opts = DefaultBackfillOptions()
	}

	// Convert wss:// or ws:// to https:// or http://
	if strings.HasPrefix(opts.RelayHost, "wss://") {
		opts.RelayHost = "https://" + opts.RelayHost[6:]
	} else if strings.HasPrefix(opts.RelayHost, "ws://") {
		opts.RelayHost = "http://" + opts.RelayHost[5:]
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
		syncLimiter:           rate.NewLimiter(rate.Limit(opts.SyncRequestsPerSecond), 1),
		RelayHost:             opts.RelayHost,
		stop:                  make(chan chan struct{}, 1),
		Directory:             identity.DefaultDirectory(),
	}
}

// Start starts the backfill processor routine
func (b *Backfiller) Start() {
	ctx := context.Background()

	log := slog.With("source", "backfiller", "name", b.Name)
	log.Info("starting backfill processor")

	sem := semaphore.NewWeighted(int64(b.ParallelBackfills))

	for {
		select {
		case stopped := <-b.stop:
			log.Info("stopping backfill processor")
			sem.Acquire(ctx, int64(b.ParallelBackfills))
			close(stopped)
			return
		default:
		}

		// Get the next job
		job, err := b.Store.GetNextEnqueuedJob(ctx)
		if err != nil {
			log.Error("failed to get next enqueued job", "error", err)
			time.Sleep(1 * time.Second)
			continue
		} else if job == nil {
			time.Sleep(1 * time.Second)
			continue
		}

		log := log.With("repo", job.Repo())

		// Mark the backfill as "in progress"
		err = job.SetState(ctx, StateInProgress)
		if err != nil {
			log.Error("failed to set job state", "error", err)
			continue
		}

		sem.Acquire(ctx, 1)
		go func(j Job) {
			defer sem.Release(1)
			newState, err := b.BackfillRepo(ctx, j)
			if err != nil {
				log.Error("failed to backfill repo", "error", err)
			}
			if newState != "" {
				if sserr := j.SetState(ctx, newState); sserr != nil {
					log.Error("failed to set job state", "error", sserr)
				}

				if strings.HasPrefix(newState, "failed") {
					// Clear buffered ops
					if err := j.ClearBufferedOps(ctx); err != nil {
						log.Error("failed to clear buffered ops", "error", err)
					}
				}
			}
			backfillJobsProcessed.WithLabelValues(b.Name).Inc()
		}(job)
	}
}

// Stop stops the backfill processor
func (b *Backfiller) Stop(ctx context.Context) error {
	log := slog.With("source", "backfiller", "name", b.Name)
	log.Info("stopping backfill processor")
	stopped := make(chan struct{})
	b.stop <- stopped
	select {
	case <-stopped:
		log.Info("backfill processor stopped")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// FlushBuffer processes buffered operations for a job
func (b *Backfiller) FlushBuffer(ctx context.Context, job Job) int {
	ctx, span := tracer.Start(ctx, "FlushBuffer")
	defer span.End()
	log := slog.With("source", "backfiller_buffer_flush", "repo", job.Repo())

	processed := 0

	repo := job.Repo()

	// Flush buffered operations, clear the buffer, and mark the job as "complete"
	// Clearing and marking are handled by the job interface
	err := job.FlushBufferedOps(ctx, func(kind repomgr.EventKind, rev, path string, rec *[]byte, cid *cid.Cid) error {
		switch kind {
		case repomgr.EvtKindCreateRecord:
			err := b.HandleCreateRecord(ctx, repo, rev, path, rec, cid)
			if err != nil {
				log.Error("failed to handle create record", "error", err)
			}
		case repomgr.EvtKindUpdateRecord:
			err := b.HandleUpdateRecord(ctx, repo, rev, path, rec, cid)
			if err != nil {
				log.Error("failed to handle update record", "error", err)
			}
		case repomgr.EvtKindDeleteRecord:
			err := b.HandleDeleteRecord(ctx, repo, rev, path)
			if err != nil {
				log.Error("failed to handle delete record", "error", err)
			}
		}
		backfillOpsBuffered.WithLabelValues(b.Name).Dec()
		processed++
		return nil
	})
	if err != nil {
		log.Error("failed to flush buffered ops", "error", err)
		if errors.Is(err, ErrEventGap) {
			if sserr := job.SetState(ctx, StateEnqueued); sserr != nil {
				log.Error("failed to reset job state after failed buffer flush", "error", sserr)
			}
			// TODO: need to re-queue this job for later
			return processed
		}
	}

	// Mark the job as "complete"
	err = job.SetState(ctx, StateComplete)
	if err != nil {
		log.Error("failed to set job state", "error", err)
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

// Fetches a repo CAR file over HTTP from the indicated host. If successful, parses the CAR and returns repo.Repo
func (b *Backfiller) fetchRepo(ctx context.Context, did, since, host string) (*repo.Repo, error) {
	url := fmt.Sprintf("%s/xrpc/com.atproto.sync.getRepo?did=%s", host, did)

	if since != "" {
		url = url + fmt.Sprintf("&since=%s", since)
	}

	// GET and CAR decode the body
	client := &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
		Timeout:   600 * time.Second,
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.ipld.car")
	req.Header.Set("User-Agent", fmt.Sprintf("atproto-backfill-%s/0.0.1", b.Name))
	if b.magicHeaderKey != "" && b.magicHeaderVal != "" {
		req.Header.Set(b.magicHeaderKey, b.magicHeaderVal)
	}

	b.syncLimiter.Wait(ctx)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		reason := "unknown error"
		if resp.StatusCode == http.StatusBadRequest {
			reason = "repo not found"
		} else {
			reason = resp.Status
		}
		return nil, fmt.Errorf("failed to get repo: %s", reason)
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

// BackfillRepo backfills a repo
func (b *Backfiller) BackfillRepo(ctx context.Context, job Job) (string, error) {
	ctx, span := tracer.Start(ctx, "BackfillRepo")
	defer span.End()

	start := time.Now()

	repoDID := job.Repo()

	log := slog.With("source", "backfiller_backfill_repo", "repo", repoDID)
	if job.RetryCount() > 0 {
		log = log.With("retry_count", job.RetryCount())
	}
	log.Info(fmt.Sprintf("processing backfill for %s", repoDID))

	var r *repo.Repo
	if b.tryRelayRepoFetch {
		rr, err := b.fetchRepo(ctx, repoDID, job.Rev(), b.RelayHost)
		if err != nil {
			slog.Warn("repo CAR fetch from relay failed", "did", repoDID, "since", job.Rev(), "relayHost", b.RelayHost, "err", err)
		} else {
			r = rr
		}
	}

	if r == nil {
		ident, err := b.Directory.LookupDID(ctx, syntax.DID(repoDID))
		if err != nil {
			return "failed resolving DID to PDS repo", fmt.Errorf("resolving DID for PDS repo fetch: %w", err)
		}
		pdsHost := ident.PDSEndpoint()
		if pdsHost == "" {
			return "DID document missing PDS endpoint", fmt.Errorf("no PDS endpoint for DID: %s", repoDID)
		}

		r, err = b.fetchRepo(ctx, repoDID, job.Rev(), pdsHost)
		if err != nil {
			slog.Warn("repo CAR fetch from PDS failed", "did", repoDID, "since", job.Rev(), "pdsHost", pdsHost, "err", err)
			return "repo CAR fetch from PDS failed", err
		}
	}

	numRecords := 0
	numRoutines := b.ParallelRecordCreates
	recordQueue := make(chan recordQueueItem, numRoutines)
	recordResults := make(chan recordResult, numRoutines)

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
					recordResults <- recordResult{recordPath: item.recordPath, err: fmt.Errorf("failed to get blocks for record: %w", err)}
					continue
				}

				raw := blk.RawData()

				err = b.HandleCreateRecord(ctx, repoDID, rev, item.recordPath, &raw, &item.nodeCid)
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
				log.Error("Error processing record", "record", result.recordPath, "error", result.err)
			}
		}
	}()

	wg.Wait()
	close(recordResults)
	resultWG.Wait()

	if err := job.SetRev(ctx, r.SignedCommit().Rev); err != nil {
		log.Error("failed to update rev after backfilling repo", "err", err)
	}

	// Process buffered operations, marking the job as "complete" when done
	numProcessed := b.FlushBuffer(ctx, job)

	log.Info("backfill complete",
		"buffered_records_processed", numProcessed,
		"records_backfilled", numRecords,
		"duration", time.Since(start),
	)

	return StateComplete, nil
}

const trust = true

func (bf *Backfiller) getRecord(ctx context.Context, r *repo.Repo, op *atproto.SyncSubscribeRepos_RepoOp) (cid.Cid, *[]byte, error) {
	if trust {
		if op.Cid == nil {
			return cid.Undef, nil, fmt.Errorf("op had no cid set")
		}

		c := (cid.Cid)(*op.Cid)
		blk, err := r.Blockstore().Get(ctx, c)
		if err != nil {
			return cid.Undef, nil, err
		}

		raw := blk.RawData()

		return c, &raw, nil
	} else {
		return r.GetRecordBytes(ctx, op.Path)
	}
}

func (bf *Backfiller) HandleEvent(ctx context.Context, evt *atproto.SyncSubscribeRepos_Commit) error {
	r, err := repo.ReadRepoFromCar(ctx, bytes.NewReader(evt.Blocks))
	if err != nil {
		return fmt.Errorf("failed to read event repo: %w", err)
	}

	ops := make([]*BufferedOp, 0, len(evt.Ops))
	for _, op := range evt.Ops {
		kind := repomgr.EventKind(op.Action)
		switch kind {
		case repomgr.EvtKindCreateRecord, repomgr.EvtKindUpdateRecord:
			cc, rec, err := bf.getRecord(ctx, r, op)
			if err != nil {
				return fmt.Errorf("getting record failed (%s,%s): %w", op.Action, op.Path, err)
			}
			ops = append(ops, &BufferedOp{
				Kind:   kind,
				Path:   op.Path,
				Record: rec,
				Cid:    &cc,
			})
		case repomgr.EvtKindDeleteRecord:
			ops = append(ops, &BufferedOp{
				Kind: kind,
				Path: op.Path,
			})
		default:
			return fmt.Errorf("invalid op action: %q", op.Action)
		}
	}

	if evt.Since == nil {
		// The first event for a repo will have a nil since, we can enqueue the repo as "complete" to avoid fetching the empty repo
		if err := bf.Store.EnqueueJobWithState(ctx, evt.Repo, StateComplete); err != nil {
			return fmt.Errorf("failed to enqueue job with state for repo %q: %w", evt.Repo, err)
		}
	}

	buffered, err := bf.BufferOps(ctx, evt.Repo, evt.Since, evt.Rev, ops)
	if err != nil {
		if errors.Is(err, ErrAlreadyProcessed) {
			return nil
		}
		return fmt.Errorf("buffer ops failed: %w", err)
	}

	if buffered {
		return nil
	}

	for _, op := range ops {
		switch op.Kind {
		case repomgr.EvtKindCreateRecord:
			if err := bf.HandleCreateRecord(ctx, evt.Repo, evt.Rev, op.Path, op.Record, op.Cid); err != nil {
				return fmt.Errorf("create record failed: %w", err)
			}
		case repomgr.EvtKindUpdateRecord:
			if err := bf.HandleUpdateRecord(ctx, evt.Repo, evt.Rev, op.Path, op.Record, op.Cid); err != nil {
				return fmt.Errorf("update record failed: %w", err)
			}
		case repomgr.EvtKindDeleteRecord:
			if err := bf.HandleDeleteRecord(ctx, evt.Repo, evt.Rev, op.Path); err != nil {
				return fmt.Errorf("delete record failed: %w", err)
			}
		}
	}

	if err := bf.Store.UpdateRev(ctx, evt.Repo, evt.Rev); err != nil {
		return fmt.Errorf("failed to update rev: %w", err)
	}

	return nil
}

func (bf *Backfiller) BufferOp(ctx context.Context, repo string, since *string, rev string, kind repomgr.EventKind, path string, rec *[]byte, cid *cid.Cid) (bool, error) {
	return bf.BufferOps(ctx, repo, since, rev, []*BufferedOp{{
		Path:   path,
		Kind:   kind,
		Record: rec,
		Cid:    cid,
	}})
}

func (bf *Backfiller) BufferOps(ctx context.Context, repo string, since *string, rev string, ops []*BufferedOp) (bool, error) {
	j, err := bf.Store.GetJob(ctx, repo)
	if err != nil {
		if !errors.Is(err, ErrJobNotFound) {
			return false, err
		}
		if qerr := bf.Store.EnqueueJob(ctx, repo); qerr != nil {
			return false, fmt.Errorf("failed to enqueue job for unknown repo: %w", qerr)
		}

		nj, err := bf.Store.GetJob(ctx, repo)
		if err != nil {
			return false, err
		}

		j = nj
	}

	return j.BufferOps(ctx, since, rev, ops)
}

// MaxRetries is the maximum number of times to retry a backfill job
var MaxRetries = 10

func computeExponentialBackoff(attempt int) time.Duration {
	return time.Duration(1<<uint(attempt)) * 10 * time.Second
}
