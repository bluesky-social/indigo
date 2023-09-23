package backfill

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ipfs/go-cid"
	typegen "github.com/whyrusleeping/cbor-gen"
	"gorm.io/gorm"
)

type Gormjob struct {
	repo        string
	state       string
	lk          sync.Mutex
	bufferedOps map[string][]*bufferedOp

	dbj *GormDBJob
	db  *gorm.DB

	createdAt time.Time
	updatedAt time.Time
}

type GormDBJob struct {
	gorm.Model
	Repo  string `gorm:"unique;index"`
	State string `gorm:"index"`
}

// Gormstore is a gorm-backed implementation of the Backfill Store interface
type Gormstore struct {
	lk   sync.RWMutex
	jobs map[string]*Gormjob
	db   *gorm.DB
}

func NewGormstore(db *gorm.DB) *Gormstore {
	return &Gormstore{
		jobs: make(map[string]*Gormjob),
		db:   db,
	}
}

func (s *Gormstore) LoadJobs(ctx context.Context) error {
	limit := 100_000
	offset := 0
	s.lk.Lock()
	defer s.lk.Unlock()

	for {
		var dbjobs []*GormDBJob
		// Load all jobs from the database
		if err := s.db.Find(&dbjobs).Offset(offset).Limit(limit).Error; err != nil {
			return err
		}
		if len(dbjobs) == 0 {
			break
		}
		offset += len(dbjobs)

		// Convert them to in-memory jobs
		for i := range dbjobs {
			dbj := dbjobs[i]
			j := &Gormjob{
				repo:        dbj.Repo,
				state:       dbj.State,
				bufferedOps: map[string][]*bufferedOp{},
				createdAt:   dbj.CreatedAt,
				updatedAt:   dbj.UpdatedAt,

				dbj: dbj,
				db:  s.db,
			}
			s.jobs[dbj.Repo] = j
		}
	}

	return nil
}

func (s *Gormstore) EnqueueJob(repo string) error {
	// Persist the job to the database
	dbj := &GormDBJob{
		Repo:  repo,
		State: StateEnqueued,
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
		repo:        repo,
		createdAt:   time.Now(),
		updatedAt:   time.Now(),
		state:       StateEnqueued,
		bufferedOps: map[string][]*bufferedOp{},

		dbj: dbj,
		db:  s.db,
	}
	s.jobs[repo] = j

	return nil
}

func (s *Gormstore) BufferOp(ctx context.Context, repo, kind, path string, rec *typegen.CBORMarshaler, cid *cid.Cid) (bool, error) {
	s.lk.RLock()

	// If the job doesn't exist, we can't buffer an op for it
	j, ok := s.jobs[repo]
	s.lk.RUnlock()
	if !ok {
		return false, ErrJobNotFound
	}

	j.lk.Lock()
	defer j.lk.Unlock()

	switch j.state {
	case StateComplete:
		return false, ErrJobComplete
	case StateInProgress:
	// keep going and buffer the op
	default:
		return false, nil
	}

	j.bufferedOps[path] = append(j.bufferedOps[path], &bufferedOp{
		kind: kind,
		rec:  rec,
		cid:  cid,
	})
	j.updatedAt = time.Now()
	return true, nil
}

func (s *Gormstore) GetJob(ctx context.Context, repo string) (Job, error) {
	s.lk.RLock()
	defer s.lk.RUnlock()

	j, ok := s.jobs[repo]
	if !ok || j == nil {
		return nil, nil
	}
	return j, nil
}

func (s *Gormstore) GetNextEnqueuedJob(ctx context.Context) (Job, error) {
	s.lk.RLock()
	defer s.lk.RUnlock()

	for _, j := range s.jobs {
		if j.State() == StateEnqueued {
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

func (j *Gormjob) SetState(ctx context.Context, state string) error {
	j.lk.Lock()
	defer j.lk.Unlock()

	j.state = state
	j.updatedAt = time.Now()

	// Persist the job to the database
	j.dbj.State = state
	return j.db.Save(j.dbj).Error
}

func (j *Gormjob) FlushBufferedOps(ctx context.Context, fn func(kind, path string, rec *typegen.CBORMarshaler, cid *cid.Cid) error) error {
	j.lk.Lock()
	defer j.lk.Unlock()

	for path, ops := range j.bufferedOps {
		for _, op := range ops {
			if err := fn(op.kind, path, op.rec, op.cid); err != nil {
				return err
			}
		}
	}

	j.bufferedOps = map[string][]*bufferedOp{}
	j.state = StateComplete

	return nil
}

func (j *Gormjob) ClearBufferedOps(ctx context.Context) error {
	j.lk.Lock()
	defer j.lk.Unlock()

	j.bufferedOps = map[string][]*bufferedOp{}
	j.updatedAt = time.Now()
	return nil
}
