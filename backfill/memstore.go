package backfill

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ipfs/go-cid"
)

type bufferedOp struct {
	kind string
	path string
	rec  *[]byte
	cid  *cid.Cid
}

type opSet struct {
	since *string
	rev   string
	ops   []*bufferedOp
}

type Memjob struct {
	repo        string
	state       string
	rev         string
	lk          sync.Mutex
	bufferedOps []*opSet

	createdAt time.Time
	updatedAt time.Time
}

// Memstore is a simple in-memory implementation of the Backfill Store interface
type Memstore struct {
	lk   sync.RWMutex
	jobs map[string]*Memjob
}

func NewMemstore() *Memstore {
	return &Memstore{
		jobs: make(map[string]*Memjob),
	}
}

func (s *Memstore) EnqueueJob(repo string) error {
	s.lk.Lock()
	defer s.lk.Unlock()

	if _, ok := s.jobs[repo]; ok {
		return fmt.Errorf("job already exists for repo %s", repo)
	}

	j := &Memjob{
		repo:      repo,
		createdAt: time.Now(),
		updatedAt: time.Now(),
		state:     StateEnqueued,
	}
	s.jobs[repo] = j
	return nil
}

func (s *Memstore) BufferOp(ctx context.Context, repo string, since *string, rev, kind, path string, rec *[]byte, cid *cid.Cid) (bool, error) {
	s.lk.Lock()

	// If the job doesn't exist, we can't buffer an op for it
	j, ok := s.jobs[repo]
	s.lk.Unlock()
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

	j.bufferedOps = append(j.bufferedOps, &opSet{
		since: since,
		rev:   rev,
		ops: []*bufferedOp{&bufferedOp{
			path: path,
			kind: kind,
			rec:  rec,
			cid:  cid,
		}},
	})
	j.updatedAt = time.Now()
	return true, nil
}

func (j *Memjob) BufferOps(ctx context.Context, since *string, rev string, ops []*bufferedOp) (bool, error) {
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

	j.bufferedOps = append(j.bufferedOps, &opSet{
		since: since,
		rev:   rev,
		ops:   ops,
	})
	j.updatedAt = time.Now()
	return true, nil
}

func (s *Memstore) GetJob(ctx context.Context, repo string) (Job, error) {
	s.lk.RLock()
	defer s.lk.RUnlock()

	j, ok := s.jobs[repo]
	if !ok || j == nil {
		return nil, nil
	}
	return j, nil
}

func (s *Memstore) GetNextEnqueuedJob(ctx context.Context) (Job, error) {
	s.lk.RLock()
	defer s.lk.RUnlock()

	for _, j := range s.jobs {
		if j.State() == StateEnqueued {
			return j, nil
		}
	}
	return nil, nil
}

func (s *Memstore) PurgeRepo(ctx context.Context, repo string) error {
	s.lk.RLock()
	defer s.lk.RUnlock()

	delete(s.jobs, repo)
	return nil
}

func (j *Memjob) Repo() string {
	return j.repo
}

func (j *Memjob) State() string {
	j.lk.Lock()
	defer j.lk.Unlock()

	return j.state
}

func (j *Memjob) SetState(ctx context.Context, state string) error {
	j.lk.Lock()
	defer j.lk.Unlock()

	j.state = state
	j.updatedAt = time.Now()
	return nil
}

func (j *Memjob) Rev() string {
	return j.rev
}

func (j *Memjob) SetRev(ctx context.Context, rev string) error {
	j.rev = rev
	return nil
}

func (j *Memjob) FlushBufferedOps(ctx context.Context, fn func(kind, rev, path string, rec *[]byte, cid *cid.Cid) error) error {
	panic("TODO: copy what we end up doing from the gormstore")
	/*
		j.lk.Lock()
		defer j.lk.Unlock()

		for _, opset := range j.bufferedOps {
			for _, op := range opset.ops {
				if err := fn(op.kind, op.path, op.rec, op.cid); err != nil {
					return err
				}
			}
		}

		j.bufferedOps = map[string][]*bufferedOp{}
		j.state = StateComplete

		return nil
	*/
}

func (j *Memjob) ClearBufferedOps(ctx context.Context) error {
	j.lk.Lock()
	defer j.lk.Unlock()

	j.bufferedOps = []*opSet{}
	j.updatedAt = time.Now()
	return nil
}

func (j *Memjob) RetryCount() int {
	j.lk.Lock()
	defer j.lk.Unlock()
	return 0
}
