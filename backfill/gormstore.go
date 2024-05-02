package backfill

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/repomgr"
	"github.com/ipfs/go-cid"
	"gorm.io/gorm"
)

type Gormjob struct {
	repo  string
	state string
	rev   string

	lk          sync.Mutex
	bufferedOps []*opSet

	dbj *GormDBJob
	db  *gorm.DB

	createdAt time.Time
	updatedAt time.Time

	retryCount int
	retryAfter *time.Time
}

type GormDBJob struct {
	gorm.Model
	Repo       string `gorm:"unique;index"`
	State      string `gorm:"index:enqueued_job_idx,where:state = 'enqueued';index:retryable_job_idx,where:state like 'failed%'"`
	Rev        string
	RetryCount int
	RetryAfter *time.Time `gorm:"index:retryable_job_idx,sort:desc"`
}

// Gormstore is a gorm-backed implementation of the Backfill Store interface
type Gormstore struct {
	lk   sync.RWMutex
	jobs map[string]*Gormjob

	qlk       sync.Mutex
	taskQueue []string

	db *gorm.DB
}

func NewGormstore(db *gorm.DB) *Gormstore {
	return &Gormstore{
		jobs: make(map[string]*Gormjob),
		db:   db,
	}
}

func (s *Gormstore) LoadJobs(ctx context.Context) error {
	s.qlk.Lock()
	defer s.qlk.Unlock()
	return s.loadJobs(ctx, 20_000)
}

func (s *Gormstore) loadJobs(ctx context.Context, limit int) error {
	enqueuedIndexClause := ""
	retryableIndexClause := ""

	// If the DB is a SQLite DB, we can use INDEXED BY to speed up the query
	if s.db.Dialector.Name() == "sqlite" {
		enqueuedIndexClause = "INDEXED BY enqueued_job_idx"
		retryableIndexClause = "INDEXED BY retryable_job_idx"
	}

	enqueuedSelect := fmt.Sprintf(`SELECT repo FROM gorm_db_jobs %s WHERE state  = 'enqueued' LIMIT ?`, enqueuedIndexClause)
	retryableSelect := fmt.Sprintf(`SELECT repo FROM gorm_db_jobs %s WHERE state like 'failed%%' AND (retry_after = NULL OR retry_after < ?) LIMIT ?`, retryableIndexClause)

	var todo []string
	if err := s.db.Raw(enqueuedSelect, limit).Scan(&todo).Error; err != nil {
		return err
	}

	if len(todo) < limit {
		var moreTodo []string
		if err := s.db.Raw(retryableSelect, time.Now(), limit-len(todo)).Scan(&moreTodo).Error; err != nil {
			return err
		}
		todo = append(todo, moreTodo...)
	}

	s.taskQueue = append(s.taskQueue, todo...)

	return nil
}

func (s *Gormstore) GetOrCreateJob(ctx context.Context, repo, state string) (Job, error) {
	j, err := s.getJob(ctx, repo)
	if err == nil {
		return j, nil
	}

	if !errors.Is(err, ErrJobNotFound) {
		return nil, err
	}

	if err := s.createJobForRepo(repo, state); err != nil {
		return nil, err
	}

	return s.getJob(ctx, repo)
}

func (s *Gormstore) EnqueueJob(ctx context.Context, repo string) error {
	_, err := s.GetOrCreateJob(ctx, repo, StateEnqueued)
	if err != nil {
		return err
	}

	s.qlk.Lock()
	s.taskQueue = append(s.taskQueue, repo)
	s.qlk.Unlock()

	return nil
}

func (s *Gormstore) EnqueueJobWithState(ctx context.Context, repo, state string) error {
	_, err := s.GetOrCreateJob(ctx, repo, state)
	if err != nil {
		return err
	}

	s.qlk.Lock()
	s.taskQueue = append(s.taskQueue, repo)
	s.qlk.Unlock()

	return nil
}

func (s *Gormstore) createJobForRepo(repo, state string) error {
	dbj := &GormDBJob{
		Repo:  repo,
		State: state,
	}
	if err := s.db.Create(dbj).Error; err != nil {
		if err == gorm.ErrDuplicatedKey {
			return nil
		}
		return err
	}

	s.lk.Lock()
	defer s.lk.Unlock()

	// Convert it to an in-memory job
	if _, ok := s.jobs[repo]; ok {
		// The DB create should have errored if the job already existed, but just in case
		return fmt.Errorf("job already exists for repo %s", repo)
	}

	j := &Gormjob{
		repo:      repo,
		createdAt: time.Now(),
		updatedAt: time.Now(),
		state:     state,

		dbj: dbj,
		db:  s.db,
	}
	s.jobs[repo] = j

	return nil
}

func (j *Gormjob) BufferOps(ctx context.Context, since *string, rev string, ops []*BufferedOp) (bool, error) {
	j.lk.Lock()
	defer j.lk.Unlock()

	switch j.state {
	case StateComplete:
		return false, nil
	case StateInProgress, StateEnqueued:
		// keep going and buffer the op
	default:
		if strings.HasPrefix(j.state, "failed") {
			if j.retryCount >= MaxRetries {
				// Process immediately since we're out of retries
				return false, nil
			}
			// Don't buffer the op since it'll get caught in the next retry (hopefully)
			return true, nil
		}
		return false, fmt.Errorf("invalid job state: %q", j.state)
	}

	if j.rev >= rev || (since == nil && j.rev != "") {
		// we've already accounted for this event
		return false, ErrAlreadyProcessed
	}

	j.bufferOps(&opSet{since: since, rev: rev, ops: ops})
	return true, nil
}

func (j *Gormjob) bufferOps(ops *opSet) {
	j.bufferedOps = append(j.bufferedOps, ops)
	j.updatedAt = time.Now()
}

func (s *Gormstore) GetJob(ctx context.Context, repo string) (Job, error) {
	return s.getJob(ctx, repo)
}

func (s *Gormstore) getJob(ctx context.Context, repo string) (*Gormjob, error) {
	cj := s.checkJobCache(ctx, repo)
	if cj != nil {
		return cj, nil
	}

	return s.loadJob(ctx, repo)
}

func (s *Gormstore) loadJob(ctx context.Context, repo string) (*Gormjob, error) {
	var dbj GormDBJob
	if err := s.db.Find(&dbj, "repo = ?", repo).Error; err != nil {
		return nil, err
	}

	if dbj.ID == 0 {
		return nil, ErrJobNotFound
	}

	j := &Gormjob{
		repo:      dbj.Repo,
		state:     dbj.State,
		createdAt: dbj.CreatedAt,
		updatedAt: dbj.UpdatedAt,

		dbj: &dbj,
		db:  s.db,

		retryCount: dbj.RetryCount,
		retryAfter: dbj.RetryAfter,
	}
	s.lk.Lock()
	defer s.lk.Unlock()
	// would imply a race condition
	exist, ok := s.jobs[repo]
	if ok {
		return exist, nil
	}
	s.jobs[repo] = j
	return j, nil
}

func (s *Gormstore) checkJobCache(ctx context.Context, repo string) *Gormjob {
	s.lk.RLock()
	defer s.lk.RUnlock()

	j, ok := s.jobs[repo]
	if !ok || j == nil {
		return nil
	}
	return j
}

func (s *Gormstore) GetNextEnqueuedJob(ctx context.Context) (Job, error) {
	s.qlk.Lock()
	defer s.qlk.Unlock()
	if len(s.taskQueue) == 0 {
		if err := s.loadJobs(ctx, 1000); err != nil {
			return nil, err
		}

		if len(s.taskQueue) == 0 {
			return nil, nil
		}
	}

	for len(s.taskQueue) > 0 {
		first := s.taskQueue[0]
		s.taskQueue = s.taskQueue[1:]

		j, err := s.getJob(ctx, first)
		if err != nil {
			return nil, err
		}

		shouldRetry := strings.HasPrefix(j.State(), "failed") && j.retryAfter != nil && time.Now().After(*j.retryAfter)

		if j.State() == StateEnqueued || shouldRetry {
			return j, nil
		}
	}
	return nil, nil
}

func (j *Gormjob) Repo() string {
	return j.repo
}

func (j *Gormjob) State() string {
	j.lk.Lock()
	defer j.lk.Unlock()

	return j.state
}

func (j *Gormjob) SetRev(ctx context.Context, r string) error {
	j.lk.Lock()
	defer j.lk.Unlock()

	j.rev = r
	j.updatedAt = time.Now()

	// Persist the job to the database

	return j.db.Save(j.dbj).Error
}

func (j *Gormjob) Rev() string {
	j.lk.Lock()
	defer j.lk.Unlock()

	return j.rev
}

func (j *Gormjob) SetState(ctx context.Context, state string) error {
	j.lk.Lock()
	defer j.lk.Unlock()

	j.state = state
	j.updatedAt = time.Now()

	if strings.HasPrefix(state, "failed") {
		if j.retryCount < MaxRetries {
			next := time.Now().Add(computeExponentialBackoff(j.retryCount))
			j.retryAfter = &next
			j.retryCount++
		} else {
			j.retryAfter = nil
		}
	}

	// Persist the job to the database
	j.dbj.State = state
	return j.db.Save(j.dbj).Error
}

func (j *Gormjob) FlushBufferedOps(ctx context.Context, fn func(kind repomgr.EventKind, rev, path string, rec *[]byte, cid *cid.Cid) error) error {
	// TODO: this will block any events for this repo while this flush is ongoing, is that okay?
	j.lk.Lock()
	defer j.lk.Unlock()

	for _, opset := range j.bufferedOps {
		if opset.rev <= j.rev {
			// stale events, skip
			continue
		}

		if opset.since == nil {
			// The first event for a repo may have a nil since
			// We should process it only if the rev is empty, skip otherwise
			if j.rev != "" {
				continue
			}
		}

		if j.rev > *opset.since {
			// we've already accounted for this event
			continue
		}

		if j.rev != *opset.since {
			// we've got a discontinuity
			return fmt.Errorf("event since did not match current rev (%s != %s): %w", *opset.since, j.rev, ErrEventGap)
		}

		for _, op := range opset.ops {
			if err := fn(op.Kind, opset.rev, op.Path, op.Record, op.Cid); err != nil {
				return err
			}
		}

		j.rev = opset.rev
	}

	j.bufferedOps = []*opSet{}
	j.state = StateComplete

	return nil
}

func (j *Gormjob) ClearBufferedOps(ctx context.Context) error {
	j.lk.Lock()
	defer j.lk.Unlock()

	j.bufferedOps = []*opSet{}
	j.updatedAt = time.Now()
	return nil
}

func (j *Gormjob) RetryCount() int {
	j.lk.Lock()
	defer j.lk.Unlock()
	return j.retryCount
}

func (s *Gormstore) UpdateRev(ctx context.Context, repo, rev string) error {
	j, err := s.GetJob(ctx, repo)
	if err != nil {
		return err
	}

	return j.SetRev(ctx, rev)
}

func (s *Gormstore) PurgeRepo(ctx context.Context, repo string) error {
	if err := s.db.Exec("DELETE FROM gorm_db_jobs WHERE repo = ?", repo).Error; err != nil {
		return err
	}

	s.lk.Lock()
	defer s.lk.Unlock()
	delete(s.jobs, repo)

	return nil
}
