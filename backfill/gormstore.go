package backfill

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/repo"
	"github.com/ipfs/go-cid"
	typegen "github.com/whyrusleeping/cbor-gen"
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
}

type GormDBJob struct {
	gorm.Model
	Repo  string `gorm:"unique;index"`
	State string `gorm:"index"`
	Rev   string
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
	limit := 20_000
	offset := 0
	s.lk.Lock()
	defer s.lk.Unlock()

	for {
		var dbjobs []*GormDBJob
		// Load all jobs from the database
		if err := s.db.Limit(limit).Offset(offset).Find(&dbjobs).Error; err != nil {
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
				repo:      dbj.Repo,
				state:     dbj.State,
				createdAt: dbj.CreatedAt,
				updatedAt: dbj.UpdatedAt,

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
		repo:      repo,
		createdAt: time.Now(),
		updatedAt: time.Now(),
		state:     StateEnqueued,

		dbj: dbj,
		db:  s.db,
	}
	s.jobs[repo] = j

	return nil
}

func (bf *Backfiller) HandleEvent(ctx context.Context, evt *atproto.SyncSubscribeRepos_Commit) error {
	r, err := repo.ReadRepoFromCar(ctx, bytes.NewReader(evt.Blocks))
	if err != nil {
		return fmt.Errorf("failed to read event repo: %w", err)
	}

	var ops []*bufferedOp
	for _, op := range evt.Ops {
		switch op.Action {
		case "create", "update":
			cc, rec, err := r.GetRecord(ctx, op.Path)
			if err != nil {
				return err
			}

			ops = append(ops, &bufferedOp{
				kind: op.Action,
				path: op.Path,
				rec:  rec,
				cid:  &cc,
			})
		case "delete":
			ops = append(ops, &bufferedOp{
				kind: op.Action,
				path: op.Path,
			})
		default:
			return fmt.Errorf("invalid op action: %q", op.Action)
		}
	}

	buffered, err := bf.BufferOps(ctx, evt.Repo, evt.Since, evt.Rev, ops)
	if err != nil {
		return fmt.Errorf("buffer ops failed: %w", err)
	}

	if buffered {
		return nil
	}

	for _, op := range ops {
		switch op.kind {
		case "create":
			if err := bf.HandleCreateRecord(ctx, evt.Repo, op.path, op.rec, op.cid); err != nil {
				return fmt.Errorf("create record failed: %w", err)
			}
		case "update":
			if err := bf.HandleUpdateRecord(ctx, evt.Repo, op.path, op.rec, op.cid); err != nil {
				return fmt.Errorf("update record failed: %w", err)
			}
		case "delete":
			if err := bf.HandleDeleteRecord(ctx, evt.Repo, op.path); err != nil {
				return fmt.Errorf("delete record failed: %w", err)
			}
		}
	}

	if err := bf.Store.UpdateRev(ctx, evt.Repo, evt.Rev); err != nil {
		return fmt.Errorf("failed to update rev: %w", err)
	}

	return nil

}

func (j *Gormjob) BufferOps(ctx context.Context, since *string, rev string, ops []*bufferedOp) (bool, error) {
	j.lk.Lock()
	defer j.lk.Unlock()

	switch j.state {
	case StateComplete:
		return false, nil
	case StateInProgress, StateEnqueued:
		// keep going and buffer the op
	default:
		return false, fmt.Errorf("invalid job state: %q", j.state)
	}

	j.bufferOps(&opSet{since: since, rev: rev, ops: ops})
	return true, nil
}

func (bf *Backfiller) BufferOp(ctx context.Context, repo string, since *string, rev, kind, path string, rec typegen.CBORMarshaler, cid *cid.Cid) (bool, error) {
	return bf.BufferOps(ctx, repo, since, rev, []*bufferedOp{&bufferedOp{
		path: path,
		kind: kind,
		rec:  rec,
		cid:  cid,
	}})
}

func (bf *Backfiller) BufferOps(ctx context.Context, repo string, since *string, rev string, ops []*bufferedOp) (bool, error) {
	j, err := bf.Store.GetJob(ctx, repo)
	if err != nil {
		return false, err
	}

	return j.BufferOps(ctx, since, rev, ops)
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
	if err := s.db.Find(&dbj, "repo = ?").Error; err != nil {
		return nil, err
	}

	if dbj.ID > 0 {
		j := &Gormjob{
			repo:      dbj.Repo,
			state:     dbj.State,
			createdAt: dbj.CreatedAt,
			updatedAt: dbj.UpdatedAt,

			dbj: &dbj,
			db:  s.db,
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

	dbj = GormDBJob{
		Repo:  repo,
		State: StateEnqueued,
	}

	if err := s.db.Create(&dbj).Error; err != nil {
		// TODO: check for unique-constraint error vs other
		jc := s.checkJobCache(ctx, repo)
		if jc == nil {
			return nil, fmt.Errorf("job cache missing after failed create: %w", err)
		}

		return jc, nil
	}

	j := &Gormjob{
		repo:      dbj.Repo,
		state:     dbj.State,
		createdAt: dbj.CreatedAt,
		updatedAt: dbj.UpdatedAt,

		dbj: &dbj,
		db:  s.db,
	}
	s.lk.Lock()
	defer s.lk.Unlock()
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

func (j *Gormjob) SetRev(ctx context.Context, r string) error {
	j.lk.Lock()
	defer j.lk.Unlock()

	j.rev = r
	return nil
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

	// Persist the job to the database
	j.dbj.State = state
	return j.db.Save(j.dbj).Error
}

func (j *Gormjob) FlushBufferedOps(ctx context.Context, fn func(kind, path string, rec typegen.CBORMarshaler, cid *cid.Cid) error) error {
	// TODO: this will block any events for this repo while this flush is ongoing, is that okay?
	j.lk.Lock()
	defer j.lk.Unlock()

	for _, opset := range j.bufferedOps {
		if opset.rev < j.rev {
			// stale events, skip
			continue
		}

		if opset.since == nil {
			// TODO: what does this mean?
			panic("TODO")
		}

		if j.rev != *opset.since {
			// we've got a discontinuity
			panic("TODO")
		}

		for _, op := range opset.ops {
			if err := fn(op.kind, op.path, op.rec, op.cid); err != nil {
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

func (s *Gormstore) UpdateRev(ctx context.Context, repo, rev string) error {
	panic("NYI")
}
