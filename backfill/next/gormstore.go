package next

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

type Gormjob struct {
	repo  string
	pds   string
	state string
	rev   string

	lk sync.Mutex

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
	PDS        string `gorm:"index;index:enqueued_pds_job_idx;index:retryable_pds_job_idx"`
	State      string `gorm:"index:enqueued_pds_job_idx,where:state = 'enqueued';index:retryable_pds_job_idx,where:state like 'failed%'"`
	Rev        string
	RetryCount int
	RetryAfter *time.Time `gorm:"index:retryable_pds_job_idx,sort:desc"`
}

type queue struct {
	qlk       sync.Mutex
	taskQueue []string
}

// Gormstore is a gorm-backed implementation of the Backfill Store interface
type Gormstore struct {
	lk   sync.RWMutex
	jobs map[string]*Gormjob

	pdsQueues map[string]*queue

	db *gorm.DB
}

func NewGormstore(db *gorm.DB) *Gormstore {
	return &Gormstore{
		jobs:      make(map[string]*Gormjob),
		pdsQueues: make(map[string]*queue),
		db:        db,
	}
}

func (s *Gormstore) LoadJobs(ctx context.Context) error {
	s.lk.Lock()
	defer s.lk.Unlock()
	return s.loadJobs(ctx, 20_000)
}

type todoJob struct {
	PDS  string
	Repo string
}

func (s *Gormstore) loadJobs(ctx context.Context, limit int) error {
	enqueuedIndexClause := ""
	retryableIndexClause := ""

	// If the DB is a SQLite DB, we can use INDEXED BY to speed up the query
	if s.db.Dialector.Name() == "sqlite" {
		enqueuedIndexClause = "INDEXED BY enqueued_pds_job_idx"
		retryableIndexClause = "INDEXED BY retryable_pds_job_idx"
	}

	enqueuedSelect := fmt.Sprintf(`SELECT pds, repo FROM gorm_db_jobs %s WHERE state  = 'enqueued' LIMIT ?`, enqueuedIndexClause)
	retryableSelect := fmt.Sprintf(`SELECT pds, repo FROM gorm_db_jobs %s WHERE state like 'failed%%' AND (retry_after = NULL OR retry_after < ?) LIMIT ?`, retryableIndexClause)

	todoJobs := make([]todoJob, 0, limit)
	if err := s.db.Raw(enqueuedSelect, limit).Scan(&todoJobs).Error; err != nil {
		return err
	}

	if len(todoJobs) < limit {
		moreTodo := make([]todoJob, 0, limit-len(todoJobs))
		if err := s.db.Raw(retryableSelect, time.Now(), limit-len(todoJobs)).Scan(&moreTodo).Error; err != nil {
			return err
		}
		todoJobs = append(todoJobs, moreTodo...)
	}

	for _, job := range todoJobs {
		if pdsQueue, ok := s.pdsQueues[job.PDS]; ok {
			pdsQueue.qlk.Lock()
			pdsQueue.taskQueue = append(pdsQueue.taskQueue, job.Repo)
			pdsQueue.qlk.Unlock()
		} else {
			s.pdsQueues[job.PDS] = &queue{
				taskQueue: []string{job.Repo},
			}
		}
	}

	return nil
}

func (s *Gormstore) loadJobsForPDS(ctx context.Context, pds string, limit int) error {
	enqueuedIndexClause := ""
	retryableIndexClause := ""

	// If the DB is a SQLite DB, we can use INDEXED BY to speed up the query
	if s.db.Dialector.Name() == "sqlite" {
		enqueuedIndexClause = "INDEXED BY enqueued_pds_job_idx"
		retryableIndexClause = "INDEXED BY retryable_pds_job_idx"
	}

	enqueuedSelect := fmt.Sprintf(`SELECT pds, repo FROM gorm_db_jobs %s WHERE pds = ? AND state  = 'enqueued' LIMIT ?`, enqueuedIndexClause)
	retryableSelect := fmt.Sprintf(`SELECT pds, repo FROM gorm_db_jobs %s WHERE pds = ? AND state like 'failed%%' AND (retry_after = NULL OR retry_after < ?) LIMIT ?`, retryableIndexClause)

	todoJobs := make([]todoJob, 0, limit)
	if err := s.db.Raw(enqueuedSelect, pds, limit).Scan(&todoJobs).Error; err != nil {
		return err
	}

	if len(todoJobs) < limit {
		moreTodo := make([]todoJob, 0, limit-len(todoJobs))
		if err := s.db.Raw(retryableSelect, pds, time.Now(), limit-len(todoJobs)).Scan(&moreTodo).Error; err != nil {
			return err
		}
		todoJobs = append(todoJobs, moreTodo...)
	}

	// A PDS Queue should always exist for a PDS
	// The lock on the PDS queue should be held by the caller
	pdsQueue := s.pdsQueues[pds]
	for _, job := range todoJobs {
		pdsQueue.taskQueue = append(pdsQueue.taskQueue, job.Repo)
	}

	return nil
}

func (s *Gormstore) GetOrCreateJob(ctx context.Context, pds, repo, state string) (Job, error) {
	j, err := s.getJob(ctx, repo)
	if err == nil {
		return j, nil
	}

	if !errors.Is(err, ErrJobNotFound) {
		return nil, err
	}

	if err := s.createJobForRepo(pds, repo, state); err != nil {
		return nil, err
	}

	return s.getJob(ctx, repo)
}

func (s *Gormstore) EnqueueJob(ctx context.Context, pds, repo string) error {
	j, err := s.GetOrCreateJob(ctx, pds, repo, StateEnqueued)
	if err != nil {
		return err
	}

	if j.State() == StateComplete {
		return nil // Job is already complete, no need to enqueue again
	}

	// Add the job to the task queue for the PDS
	s.lk.Lock()
	pdsQueue, ok := s.pdsQueues[pds]
	if !ok {
		pdsQueue = &queue{
			taskQueue: []string{repo},
		}
		s.pdsQueues[pds] = pdsQueue
		s.lk.Unlock()
	} else {
		s.lk.Unlock()
		pdsQueue.qlk.Lock()
		pdsQueue.taskQueue = append(pdsQueue.taskQueue, repo)
		pdsQueue.qlk.Unlock()
	}

	return nil
}

func (s *Gormstore) EnqueueJobWithState(ctx context.Context, pds, repo, state string) error {
	_, err := s.GetOrCreateJob(ctx, pds, repo, state)
	if err != nil {
		return err
	}

	// Add the job to the task queue for the PDS
	s.lk.Lock()
	pdsQueue, ok := s.pdsQueues[pds]
	if !ok {
		pdsQueue = &queue{
			taskQueue: []string{repo},
		}
		s.pdsQueues[pds] = pdsQueue
		s.lk.Unlock()
	} else {
		s.lk.Unlock()
		pdsQueue.qlk.Lock()
		pdsQueue.taskQueue = append(pdsQueue.taskQueue, repo)
		pdsQueue.qlk.Unlock()
	}

	return nil
}

func (s *Gormstore) createJobForRepo(pds, repo, state string) error {
	dbj := &GormDBJob{
		Repo:  repo,
		PDS:   pds,
		State: state,
	}
	if err := s.db.Create(dbj).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
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
		pds:       pds,
		createdAt: time.Now(),
		updatedAt: time.Now(),
		state:     state,

		dbj: dbj,
		db:  s.db,
	}
	s.jobs[repo] = j

	return nil
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
		pds:       dbj.PDS,
		state:     dbj.State,
		rev:       dbj.Rev,
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

func (s *Gormstore) GetNextEnqueuedJob(ctx context.Context, pds string) (Job, error) {
	s.lk.Lock()
	pdsQueue, ok := s.pdsQueues[pds]
	if !ok {
		pdsQueue = &queue{
			taskQueue: []string{},
		}
		s.pdsQueues[pds] = pdsQueue
	}
	s.lk.Unlock()
	pdsQueue.qlk.Lock()
	defer pdsQueue.qlk.Unlock()

	if len(pdsQueue.taskQueue) == 0 {
		if err := s.loadJobsForPDS(ctx, pds, 1000); err != nil {
			return nil, err
		}

		if len(pdsQueue.taskQueue) == 0 {
			return nil, nil
		}
	}

	for len(pdsQueue.taskQueue) > 0 {
		first := pdsQueue.taskQueue[0]
		pdsQueue.taskQueue = pdsQueue.taskQueue[1:]

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

// MaxRetries is the maximum number of times to retry a backfill job
var MaxRetries = 10

func computeExponentialBackoff(attempt int) time.Duration {
	return time.Duration(1<<uint(attempt)) * 10 * time.Second
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

func (j *Gormjob) RetryCount() int {
	j.lk.Lock()
	defer j.lk.Unlock()
	return j.retryCount
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
