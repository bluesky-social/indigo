package backfill

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ipfs/go-cid"
	typegen "github.com/whyrusleeping/cbor-gen"
)

type bufferedOp struct {
	kind string
	rec  *typegen.CBORMarshaler
	cid  *cid.Cid
}

type Memjob struct {
	repo        string
	state       string
	lk          sync.Mutex
	bufferedOps map[string][]*bufferedOp

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
		repo:        repo,
		createdAt:   time.Now(),
		updatedAt:   time.Now(),
		state:       StateEnqueued,
		bufferedOps: map[string][]*bufferedOp{},
	}
	s.jobs[repo] = j
	return nil
}

func (s *Memstore) BufferOp(ctx context.Context, repo, kind, path string, rec *typegen.CBORMarshaler, cid *cid.Cid) (bool, error) {
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

	j.bufferedOps[path] = append(j.bufferedOps[path], &bufferedOp{
		kind: kind,
		rec:  rec,
		cid:  cid,
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

func (j *Memjob) FlushBufferedOps(ctx context.Context, fn func(kind, path string, rec *typegen.CBORMarshaler, cid *cid.Cid) error) error {
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

func (j *Memjob) ClearBufferedOps(ctx context.Context) error {
	j.lk.Lock()
	defer j.lk.Unlock()

	j.bufferedOps = map[string][]*bufferedOp{}
	j.updatedAt = time.Now()
	return nil
}
